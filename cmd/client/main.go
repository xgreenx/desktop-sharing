package main

import "C"
import (
	"bufio"
	"context"
	"fmt"
	"github.com/ipfs/go-log"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/xgreenx/desktop-sharing/src/config"
	"github.com/xgreenx/desktop-sharing/src/sharingnode"
	"os"
	"strings"
)

func ScanInputCommands(node *sharingnode.SharingNode) {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		arg := strings.Split(scanner.Text(), " ")

		switch arg[0] {
		case "list":
			node.PrintList()
		case "screen":
			if len(arg) < 2 {
				fmt.Println("Missed node ic")
				continue
			}

			id, err := peer.IDB58Decode(arg[1])
			if err != nil || id == "" {
				fmt.Println("Wrong id of node ", err)
				continue
			}

			err = node.ShareScreen(id)
			if err != nil {
				fmt.Println("Got error during sharing ", err)
				continue
			}
		default:
			fmt.Println("Unknown command ", arg[0])
		}
	}
}

func main() {
	conf := config.NewSharingConfig(config.NewBootstrapConfig())

	err := conf.ParseFlags()
	if err != nil {
		panic(err)
	}

	err = conf.LoadConfig()
	if err != nil {
		panic(err)
	}

	conf.UpdateDefaults()

	err = conf.WriteConfig()
	if err != nil {
		panic(err)
	}
	log.SetLogLevel("node", conf.LoggingLevel.String())
	log.SetLogLevel("sharingnode", conf.LoggingLevel.String())
	log.SetLogLevel("autorelay", conf.LoggingLevel.String())

	ctx := context.Background()
	node := sharingnode.NewSharingNode(ctx, conf)
	node.BootStrap()
	//
	//peerId, _ := peer.Decode("12D3KooWEj6GxaVrmKWEciRjQkBfEPvTqMyNxtBmmzvnNkavCo18")
	//node.ShareScreen(peerId)
	//
	//select {}
	ScanInputCommands(node)
}
