package node

import (
	"github.com/libp2p/go-libp2p-core/protocol"
	"github.com/xgreenx/desktop-sharing/src/config"
	"sync"
)

type ConsoleAllower struct {
	sync.Mutex
	ConnectionAllower
}

func (a ConsoleAllower) Allow(c *ConnectionInfo) (AllowResult, error) {
	// TODO: Decide how to ask user about access
	return AllowResult{
		map[protocol.ID]bool{
			protocol.ID(config.CommandID): true,
		},
		true,
	}, nil
}
