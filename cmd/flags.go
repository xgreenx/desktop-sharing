package main

import (
	"flag"
	sharing "github.com/xgreenx/desktop-sharing"
)

func ParseFlags() (*sharing.Config, error) {
	config := sharing.Config{}
	flag.Var(&config.BootstrapPeers, "peer", "Adds a peer multiaddress to the bootstrap list")
	flag.Var(&config.ListenAddresses, "listen", "Adds a multiaddress to the listen list")
	flag.StringVar(&config.ScreenProtocolId, "pid", "/screen/1.0.0", "Sets a screen protocol id for stream headers")
	flag.Parse()

	if len(config.ListenAddresses) == 0 {
		err := config.ListenAddresses.Set("/ip4/127.0.0.1/tcp/1488")

		if err != nil {
			return nil, err
		}
	}

	return &config, nil
}
