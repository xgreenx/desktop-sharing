package node

import (
	"context"
	"fmt"
	"github.com/ipfs/go-log"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-autonat-svc"
	"github.com/libp2p/go-libp2p-circuit"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/protocol"
	"github.com/libp2p/go-libp2p-core/routing"
	"github.com/libp2p/go-libp2p-discovery"
	"github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p-kad-dht/opts"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
	"github.com/multiformats/go-multiaddr"
	"github.com/xgreenx/desktop-sharing/src/config"
	"os"
	"sync"
	"time"
)

const NODES_TAG = "screen_sharing_nodes"

var logger = log.Logger("node")

type Node struct {
	Context        context.Context
	Config         *config.BootstrapConfig
	RoutingDht     *dht.IpfsDHT
	DataDht        *dht.IpfsDHT
	Host           host.Host
	AccessVerifier *AccessVerifier
	AccessStore    *AccessStore
	PingService    *ping.PingService
}

func NewNode(ctx context.Context, config *config.BootstrapConfig) *Node {
	return &Node{
		ctx,
		config,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	}
}

func nameKey(ID peer.ID) string {
	return fmt.Sprintf("/%s/name", ID.String())
}

func (n *Node) BootStrap() {
	var err error
	relayOpt := make([]relay.RelayOpt, 0)

	logger.Debug("Hop: %s", n.Config.Hop)
	if n.Config.Hop {
		relayOpt = append(relayOpt, relay.OptHop)
	}

	staticRelays := make([]peer.AddrInfo, len(n.Config.BootstrapPeers))
	for i, peerAddr := range n.Config.BootstrapPeers {
		info, err := peer.AddrInfoFromP2pAddr(peerAddr)
		if err != nil {
			panic(err)
		}
		staticRelays[i] = *info
	}

	n.Host, err = libp2p.New(n.Context,
		libp2p.ListenAddrs([]multiaddr.Multiaddr(n.Config.ListenAddresses)...),
		libp2p.Identity(n.Config.PrivateKey),
		libp2p.NATPortMap(),
		libp2p.EnableRelay(relayOpt...),
		libp2p.StaticRelays(staticRelays),
		libp2p.EnableAutoRelay(),
		libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
			n.RoutingDht, err = dht.New(n.Context, h)
			return n.RoutingDht, err
		}),
	)
	if err != nil {
		panic(err)
	}
	fmt.Println("Host created. We are:", n.Host.ID())
	logger.Info(n.Host.Addrs())

	if n.Config.Hop {
		_, err = autonat.NewAutoNATService(n.Context, n.Host)
	}

	n.PingService = ping.NewPingService(n.Host)
	n.DataDht, err = dht.New(n.Context, n.Host, dhtopts.Validator(NullValidator{}))
	if err != nil {
		panic(err)
	}

	logger.Debug("Bootstrapping the DHT")
	if err = n.RoutingDht.Bootstrap(n.Context); err != nil {
		panic(err)
	}
	if err = n.DataDht.Bootstrap(n.Context); err != nil {
		panic(err)
	}

	n.connectBootstrap()

	go func() {
		for range time.Tick(time.Minute) {
			n.connectBootstrap()
		}
	}()

	logger.Info("Announcing ourselves...")
	routingDiscovery := discovery.NewRoutingDiscovery(n.RoutingDht)
	discovery.Advertise(n.Context, routingDiscovery, NODES_TAG)
	name, err := os.Hostname()
	if err != nil {
		panic(err)
	}
	err = n.DataDht.PutValue(n.Context, nameKey(n.Host.ID()), []byte(name), dht.Quorum(1))
	if err != nil {
		logger.Error(err)
	}
	logger.Debug("Successfully announced!")

	for _, p := range n.Config.Protocols {
		switch p {
		case config.CommandID:
			n.Host.SetStreamHandler(protocol.ID(p), n.handleCommandStream)
		}
	}

	n.AccessStore = NewAccessStore(n.Config.Path)
	err = n.AccessStore.LoadRights()
	if err != nil {
		logger.Error(err)
	}
	n.AccessVerifier = NewAccessVerifier(n.AccessStore, &ConsoleAllower{}, n.Host, n.Context, n.DataDht)
}

func (n *Node) handleCommandStream(stream network.Stream) {
	defer stream.Close()
	result, err := n.AccessVerifier.Verify(stream)
	if err != nil {
		logger.Error(err, result)
	}
	// TODO: Handle console commands
}

func (n *Node) connectBootstrap() {
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
}

type FoundNode struct {
	Id   peer.ID
	Name string
	Ping int
}

func (n *Node) NodeList() chan *FoundNode {
	logger.Debug("Searching for other peers...")
	routingDiscovery := discovery.NewRoutingDiscovery(n.RoutingDht)
	peerChan, err := routingDiscovery.FindPeers(n.Context, NODES_TAG)
	if err != nil {
		logger.Error(err)
	}

	chFound := make(chan *FoundNode)

	go func() {
		fmt.Println("Starting search")
		for p := range peerChan {
			if p.ID.String() == n.Host.ID().String() {
				continue
			}
			logger.Debug(p)

			name, err := n.DataDht.GetValue(n.Context, nameKey(p.ID), dht.Quorum(1))
			if err != nil {
				logger.Warning(err)
			}

			childCtx, cancel := context.WithCancel(n.Context)
			latency := <-n.PingService.Ping(childCtx, p.ID)
			cancel()

			ping := latency.RTT
			chFound <- &FoundNode{
				Id:   p.ID,
				Name: string(name),
				Ping: int(ping.Milliseconds()),
			}
			fmt.Printf("Id: %s, latency: %s, name: %s (%s)\n", p.ID, latency.RTT, name, latency.Error)
		}
		fmt.Println("End search")
		close(chFound)
	}()

	return chFound
}
