package config

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"github.com/libp2p/go-libp2p-core/crypto"
	maddr "github.com/multiformats/go-multiaddr"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/whyrusleeping/go-logging"
	"io/ioutil"
	"os"
)

const CommandID = "/command/1.0.0"

type RemoteNode struct {
	Name   string
	Rights map[string]bool
}

// A new type we need for writing a custom flag parser
type addrList []maddr.Multiaddr

type BootstrapConfig struct {
	BootstrapPeers     addrList
	ListenAddresses    addrList
	Protocols          []string
	PrivateKey         crypto.PrivKey
	Hop                bool
	LoggingLevel       logging.Level
	AllowedConnections map[string]RemoteNode
	Viper              *viper.Viper
	configPath         string
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
	v := viper.New()

	bs, _ := hex.DecodeString(randomHex(32))
	privateKey, _, _ := crypto.GenerateEd25519Key(bytes.NewBuffer(bs))

	config := &BootstrapConfig{
		Viper:              v,
		configPath:         ConfigPath,
		AllowedConnections: make(map[string]RemoteNode),
		Hop:                false,
		LoggingLevel:       logging.ERROR,
		PrivateKey:         privateKey,
		Protocols: []string{
			CommandID,
		},
		ListenAddresses: stringsToAddrs([]string{
			"/ip4/0.0.0.0/tcp/1488",
		}),
		BootstrapPeers: stringsToAddrs([]string{
			"/ip4/194.9.70.102/tcp/1488/p2p/12D3KooWGA2HXdU4Lx81ak8XcToTKpRTGp4ghxiQsjdvAH2jarUe",
		}),
	}

	v.SetConfigName(ConfigName)
	v.SetConfigType(ConfigType)
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
	v.SetDefault("allowedConnection", &b.AllowedConnections)
}

func (b *BootstrapConfig) LoadConfig() error {
	b.Viper.AddConfigPath(b.configPath)
	if err := b.Viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			return b.WriteConfig()
		} else {
			return err
		}
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
	b.Protocols = b.Viper.GetStringSlice("protocols")

	return b.Viper.UnmarshalKey("allowedConnection", &b.AllowedConnections)
}

func (b *BootstrapConfig) ParseFlags() error {
	flag.String("listen", "/ip4/0.0.0.0/tcp/1488", "Adds a multiaddress to the listen list")
	flag.String("privateKey", "", "Private key of node")
	flag.StringVar(&b.configPath, "config", ConfigPath, "Path to config file")
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()

	var err error
	err = b.Viper.BindPFlags(pflag.CommandLine)
	if err != nil {
		return err
	}

	return nil
}

func (b *BootstrapConfig) WriteConfig() error {
	b.Viper.AddConfigPath(b.configPath)
	if err := b.Viper.WriteConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			err := os.MkdirAll(b.configPath, 0777)
			if err != nil {
				return err
			}

			err = ioutil.WriteFile(fmt.Sprintf("%s/%s.%s", b.configPath, ConfigName, ConfigType), []byte{}, 0777)
			if err != nil {
				return err
			}

			err = b.Viper.WriteConfig()
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}

	return nil
}
