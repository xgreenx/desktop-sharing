package node

import (
	"bytes"
	"context"
	"encoding/hex"
	"github.com/ipfs/go-log"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-autonat-svc"
	"github.com/libp2p/go-libp2p-circuit"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/routing"
	"github.com/libp2p/go-libp2p-discovery"
	"github.com/libp2p/go-libp2p-kad-dht"
	"github.com/multiformats/go-multiaddr"
	"github.com/xgreenx/desktop-sharing/src/config"
	"sync"
)

const NODES_TAG = "screen_sharing_nodes"

var logger = log.Logger("node")

type Node struct {
	Context context.Context
	Config  *config.Config
	Dht     *dht.IpfsDHT
	Host    host.Host
}

func NewNode(ctx context.Context, config *config.Config) *Node {
	return &Node{
		ctx,
		config,
		nil,
		nil,
	}
}

func (n *Node) BootStrap() {
	var err error
	b, err := hex.DecodeString(n.Config.PrivateKey)
	if err != nil {
		panic(err)
	}

	priv, _, err := crypto.GenerateEd25519Key(bytes.NewBuffer(b))
	if err != nil {
		panic(err)
	}

	relayOpt := make([]relay.RelayOpt, 0)

	logger.Debug("Hop:", n.Config.Hop)
	if n.Config.Hop {
		relayOpt = append(relayOpt, relay.OptHop)
	}

	n.Host, err = libp2p.New(n.Context,
		libp2p.ListenAddrs([]multiaddr.Multiaddr(n.Config.ListenAddresses)...),
		libp2p.Identity(priv),
		libp2p.NATPortMap(),
		libp2p.EnableRelay(relayOpt...),
		libp2p.EnableAutoRelay(),
		libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
			n.Dht, err = dht.New(n.Context, h)
			return n.Dht, err
		}),
	)
	if err != nil {
		panic(err)
	}
	logger.Info("Host created. We are:", n.Host.ID())
	logger.Info(n.Host.Addrs())

	if n.Config.Hop {
		_, err = autonat.NewAutoNATService(n.Context, n.Host)
	}

	logger.Debug("Bootstrapping the DHT")
	if err = n.Dht.Bootstrap(n.Context); err != nil {
		panic(err)
	}

	var wg sync.WaitGroup
	for _, peerAddr := range n.Config.BootstrapPeers {
		peerinfo, err := peer.AddrInfoFromP2pAddr(peerAddr)

		if err != nil {
			panic(err)
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := n.Host.Connect(n.Context, *peerinfo); err != nil {
				logger.Warning(err)
			} else {
				logger.Info("Connection established with bootstrap node:", *peerinfo)
			}
		}()
	}
	wg.Wait()

	logger.Info("Announcing ourselves...")
	routingDiscovery := discovery.NewRoutingDiscovery(n.Dht)
	discovery.Advertise(n.Context, routingDiscovery, NODES_TAG)
	logger.Debug("Successfully announced!")
}

func (n *Node) List() []peer.AddrInfo {
	logger.Debug("Searching for other peers...")
	routingDiscovery := discovery.NewRoutingDiscovery(n.Dht)
	peerChan, err := routingDiscovery.FindPeers(n.Context, NODES_TAG)
	if err != nil {
		panic(err)
	}

	result := make([]peer.AddrInfo, 0)

	for p := range peerChan {
		result = append(result, p)
	}

	return result
}
