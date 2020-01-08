package main

import (
	"context"
	"github.com/ipfs/go-log"
	"github.com/xgreenx/desktop-sharing/src/config"
	"github.com/xgreenx/desktop-sharing/src/node"
)

func main() {
	conf := config.NewBootstrapConfig()
	conf.Viper.SetDefault("hop", "true")

	err := conf.ParseFlags()
	if err != nil {
		panic(err)
	}

	err = conf.LoadConfig()
	if err != nil {
		panic(err)
	}

	err = conf.Viper.WriteConfig()
	if err != nil {
		panic(err)
	}
	log.SetLogLevel("node", conf.LoggingLevel.String())
	log.SetLogLevel("autorelay", conf.LoggingLevel.String())

	ctx := context.Background()

	node := node.NewNode(ctx, conf)
	node.BootStrap()

	select {}
}
