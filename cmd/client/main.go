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
			}

			id, err := peer.IDB58Decode(arg[1])
			if err != nil || id == "" {
				fmt.Println("Wrong id of node ", err)
			}

			err = node.ShareScreen(id)
			if err != nil {
				fmt.Println("Got error during sharing ", err)
			}
		default:
			fmt.Println("Unknown command ", arg[0])
		}
	}
}

func main() {
	conf := config.NewBootstrapConfig()

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
	log.SetLogLevel("sharingnode", conf.LoggingLevel.String())
	log.SetLogLevel("autorelay", conf.LoggingLevel.String())

	ctx := context.Background()
	node := sharingnode.NewSharingNode(ctx, conf)
	node.BootStrap()

	//if len(node.Config.BootstrapPeers) > 0 {
	//	peerinfo, _ := peer.AddrInfoFromP2pAddr(node.Config.BootstrapPeers[0])
	//	node.ShareScreen(peerinfo.ID)
	//}
	//
	//select {}
	ScanInputCommands(node)
}
