package node

import (
	"context"
	"errors"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/protocol"
	"github.com/libp2p/go-libp2p-kad-dht"
)

type ConnectionInfo struct {
	Rights   AccessRights
	Protocol protocol.ID
}

type AllowResult struct {
	Protocols map[protocol.ID]bool
	Remember  bool
}

func NewAllowResult() AllowResult {
	return AllowResult{
		Remember:  false,
		Protocols: make(map[protocol.ID]bool),
	}
}

type ConnectionAllower interface {
	Allow(*ConnectionInfo) (AllowResult, error)
}

type AccessVerifier struct {
	Allower ConnectionAllower
	Store   *AccessStore
	Host    host.Host
	Context context.Context
	Data    *dht.IpfsDHT
}

func NewAccessVerifier(
	store *AccessStore,
	allower ConnectionAllower,
	host host.Host,
	ctx context.Context,
	dataDht *dht.IpfsDHT) *AccessVerifier {

	verifier := &AccessVerifier{
		Store:   store,
		Allower: allower,
		Host:    host,
		Context: ctx,
		Data:    dataDht,
	}

	return verifier
}

func (a *AccessVerifier) Access(peerId peer.ID, protocolId protocol.ID) (network.Stream, error) {
	return a.Host.NewStream(a.Context, peerId, protocolId)
}

func (a *AccessVerifier) Verify(stream network.Stream) (bool, error) {
	id := stream.Conn().RemotePeer()
	if id.String() == "" {
		return false, errors.New("Can't get peer id from stream")
	}

	name, err := a.Data.GetValue(a.Context, nameKey(id), dht.Quorum(1))
	if err != nil {
		logger.Warning(err)
	}

	rights := a.Store.GetAccess(id)
	remember := false
	defer func() {
		err := a.Store.ReturnAccess(rights, remember)
		if err != nil {
			logger.Warning(err)
		}
	}()

	if !rights.IsAllowed(stream.Protocol()) {
		rights.SetName(string(name))
		connectionInfo := &ConnectionInfo{
			rights,
			stream.Protocol(),
		}

		result, err := a.Allower.Allow(connectionInfo)
		if err != nil {
			return false, err
		}

		for p, allow := range result.Protocols {
			if allow {
				rights.Allow(p)
			} else {
				rights.Deny(p)
			}
		}

		if result.Remember {
			remember = true
		}
	}

	return rights.IsAllowed(stream.Protocol()), nil
}
