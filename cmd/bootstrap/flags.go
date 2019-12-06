package main

import (
	"flag"
	"github.com/xgreenx/desktop-sharing/src/config"
)

func ParseFlags() (*config.Config, error) {
	config := config.Config{}
	flag.Var(&config.BootstrapPeers, "peer", "Adds a peer multiaddress to the bootstrap list")
	flag.Var(&config.ListenAddresses, "listen", "Adds a multiaddress to the listen list")
	flag.StringVar(&config.ScreenProtocolId, "pid", "/screen/1.0.0", "Sets a screen protocol id for stream headers")
	flag.StringVar(&config.PrivateKey, "key", "133A78928E7BD57797094A62BF711CBBE5EEEB0547CB6096D1B697ED98F79271", "Sets a private key of node")
	flag.Parse()
	config.Hop = true

	if len(config.ListenAddresses) == 0 {
		err := config.ListenAddresses.Set("/ip4/127.0.0.1/tcp/1488")

		if err != nil {
			return nil, err
		}
	}

	return &config, nil
}
