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
	"strconv"
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
				fmt.Println("Missed node id")
				continue
			}

			id, err := peer.IDB58Decode(arg[1])
			if err != nil || id == "" {
				fmt.Println("Wrong id of node ", err)
				continue
			}

			targetDisplay := 0

			if len(arg) > 2 {
				t, err := strconv.ParseUint(arg[2], 10, 0)
				if err != nil {
					fmt.Println(fmt.Sprintf("Wrong target display %s, will use targetDisplay = %d", err.Error(), targetDisplay))
				} else {
					targetDisplay = int(t)
				}
			}

			control := true
			if len(arg) > 3 {
				c, err := strconv.ParseBool(arg[3])
				if err != nil {
					fmt.Println(fmt.Sprintf("Wrong control varibale %s, will use control = %s", err.Error(), control))
				} else {
					control = c
				}
			}

			err = node.ShareScreen(id, targetDisplay, control)
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
