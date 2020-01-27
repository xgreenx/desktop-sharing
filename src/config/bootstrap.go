package config

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/protocol"
	maddr "github.com/multiformats/go-multiaddr"
	"github.com/spf13/pflag"
	"github.com/whyrusleeping/go-logging"
	"os"
)

const CommandID = protocol.ID("/command/1.0.0")

// A new type we need for writing a custom flag parser
type addrList []maddr.Multiaddr

type BootstrapConfig struct {
	*Config
	BootstrapPeers  addrList
	ListenAddresses addrList
	Protocols       []protocol.ID
	PrivateKey      crypto.PrivKey
	Hop             bool
	LoggingLevel    logging.Level
}

func randomHex(n int) string {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		panic(err)
	}
	return hex.EncodeToString(bytes)
}

func stringsToAddrs(addrStrings []string) (maddrs []maddr.Multiaddr) {
	for _, addrString := range addrStrings {
		addr, err := maddr.NewMultiaddr(addrString)
		if err != nil {
			panic(err)
		}
		maddrs = append(maddrs, addr)
	}
	return
}

const ConfigName = "BootstrapConfig"

var HomePath, _ = os.UserHomeDir()
var ConfigPath = fmt.Sprintf("%s/.desktop-sharing", HomePath)

const ConfigType = "yaml"

func NewBootstrapConfig() *BootstrapConfig {
	bs, _ := hex.DecodeString(randomHex(32))
	privateKey, _, _ := crypto.GenerateEd25519Key(bytes.NewBuffer(bs))

	config := &BootstrapConfig{
		Config:       NewConfig(ConfigPath, ConfigName, ConfigType),
		Hop:          false,
		LoggingLevel: logging.ERROR,
		PrivateKey:   privateKey,
		Protocols: []protocol.ID{
			CommandID,
		},
		ListenAddresses: stringsToAddrs([]string{
			"/ip4/0.0.0.0/tcp/1488",
		}),
		BootstrapPeers: stringsToAddrs([]string{
			"/ip4/194.9.70.102/tcp/1488/p2p/12D3KooWGA2HXdU4Lx81ak8XcToTKpRTGp4ghxiQsjdvAH2jarUe",
		}),
	}
	config.UpdateDefaults()

	return config
}

func (b *BootstrapConfig) UpdateDefaults() {
	bs, _ := b.PrivateKey.Raw()
	v := b.Viper
	v.SetDefault("privateKey", hex.EncodeToString(bs[:32]))
	v.SetDefault("hop", b.Hop)
	v.SetDefault("logging", b.LoggingLevel.String())
	v.SetDefault("protocols", b.Protocols)
	v.SetDefault("listen", b.ListenAddresses)
	v.SetDefault("bootstrap", b.BootstrapPeers)
}

func (b *BootstrapConfig) LoadConfig() error {
	err := b.Config.LoadConfig()
	if err != nil {
		return err
	}

	bs, err := hex.DecodeString(b.Viper.GetString("privateKey"))
	if err != nil {
		return err
	}

	b.PrivateKey, _, _ = crypto.GenerateEd25519Key(bytes.NewBuffer(bs))
	if err != nil {
		return err
	}

	b.Hop = b.Viper.GetBool("hop")
	b.LoggingLevel, _ = logging.LogLevel(b.Viper.GetString("logging"))
	b.BootstrapPeers = stringsToAddrs(b.Viper.GetStringSlice("bootstrap"))
	b.ListenAddresses = stringsToAddrs(b.Viper.GetStringSlice("listen"))
	b.Protocols = protocol.ConvertFromStrings(b.Viper.GetStringSlice("protocols"))

	return nil
}

func (b *BootstrapConfig) ParseFlags() error {
	flag.String("listen", "/ip4/0.0.0.0/tcp/1488", "Adds a multiaddress to the listen list")
	flag.String("privateKey", "", "Private key of node")
	flag.StringVar(&b.Path, "config", ConfigPath, "Path to config file")
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()

	var err error
	err = b.Viper.BindPFlags(pflag.CommandLine)
	if err != nil {
		return err
	}

	return nil
}
