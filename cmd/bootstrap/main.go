package main

import (
	"context"
	"github.com/ipfs/go-log"
	"github.com/whyrusleeping/go-logging"
	"github.com/xgreenx/desktop-sharing/src/node"
)

func main() {
	log.SetAllLoggers(logging.DEBUG)
	log.SetLogLevel("node", "debug")
	log.SetLogLevel("autorelay", "debug")
	config, err := ParseFlags()
	if err != nil {
		panic(err)
	}

	ctx := context.Background()

	node := node.NewNode(ctx, config)
	node.BootStrap()

	select {}
}
