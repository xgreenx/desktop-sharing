package node

import (
	"errors"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/protocol"
	"github.com/xgreenx/desktop-sharing/src/config"
	"math/rand"
	"strings"
	"sync"
	"time"
)

const accessTimeout = time.Minute * 10

func getProtocolName(id protocol.ID) string {
	return strings.Split(string(id), "/")[1]
}

type AccessRights interface {
	Name() string
	SetName(string)
	Id() peer.ID
	IsAllowed(protocol.ID) bool
	Allow(protocol.ID)
	Deny(protocol.ID)
}

type Rights struct {
	PeerName string
	PeerId   string
	Rights   map[string]bool
}

func (r *Rights) Name() string {
	return r.PeerName
}

func (r *Rights) SetName(name string) {
	r.PeerName = name
}

func (r *Rights) Id() peer.ID {
	id, err := peer.IDB58Decode(r.PeerId)
	if err != nil {
		panic(err)
	}
	return id
}

func (r *Rights) IsAllowed(id protocol.ID) bool {
	result, ok := r.Rights[getProtocolName(id)]
	return result && ok
}

func (r *Rights) Allow(id protocol.ID) {
	r.Rights[getProtocolName(id)] = true
}

func (r *Rights) Deny(id protocol.ID) {
	r.Rights[getProtocolName(id)] = false
}

type TemporaryRights struct {
	sync.Mutex
	Rights
	tokenDeadline time.Time
	tokenId       uint64
}

type AccessStore struct {
	*config.Config
	sync.RWMutex
	rights          map[string]Rights
	temporaryRights map[string]TemporaryRights
}

func NewAccessStore(path string) *AccessStore {
	a := &AccessStore{
		Config:          config.NewConfig(path, "access_store", config.ConfigType),
		rights:          make(map[string]Rights),
		temporaryRights: make(map[string]TemporaryRights),
	}
	a.Viper.SetDefault("rights", a.rights)
	return a
}

func (a *AccessStore) LoadRights() error {
	a.Lock()
	defer a.Unlock()
	err := a.LoadConfig()
	if err != nil {
		return err
	}
	return a.Viper.UnmarshalKey("rights", &a.rights)
}

func (a *AccessStore) DumpRights() error {
	a.Lock()
	defer a.Unlock()
	a.Viper.Set("rights", a.rights)
	return a.WriteConfig()
}

func (a *AccessStore) GetAccess(id peer.ID) AccessRights {
Begin:
	idS := strings.ToLower(id.String())
	a.Lock()
	tRights, ok := a.temporaryRights[idS]
	if !ok || time.Now().After(tRights.tokenDeadline) {
		rights, ok := a.rights[idS]

		if !ok {
			rights = Rights{
				PeerId: id.String(),
				Rights: make(map[string]bool),
			}
		}

		tRights = TemporaryRights{
			Rights:        rights,
			tokenDeadline: time.Now().Add(accessTimeout),
			tokenId:       rand.Uint64(),
		}
	}
	a.temporaryRights[idS] = tRights
	a.Unlock()
	tRights.Lock()
	if time.Now().After(tRights.tokenDeadline) {
		tRights.Unlock()
		goto Begin
	}

	return &tRights
}

func (a *AccessStore) ReturnAccess(access AccessRights, remember bool) error {
	tRights, ok := access.(*TemporaryRights)
	if !ok {
		return errors.New("Unknown AccessRights type")
	}
	defer tRights.Unlock()
	idS := strings.ToLower(tRights.PeerId)

	a.Lock()
	t, ok := a.temporaryRights[idS]
	if !ok {
		a.Unlock()
		return errors.New("AccessRights doesn't exist in temporary array")
	}

	if t.tokenId != tRights.tokenId {
		a.Unlock()
		return errors.New("AccessRights conains unknown token id")
	}

	if remember {
		a.rights[idS] = tRights.Rights
	}
	a.Unlock()

	if remember {
		return a.DumpRights()
	}

	return nil
}
