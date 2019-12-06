package main

import "C"
import (
	"bufio"
	"context"
	"fmt"
	"github.com/ipfs/go-log"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/whyrusleeping/go-logging"
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
			fmt.Println(node.List())
		case "screen":
			id, err := peer.IDB58Decode(arg[1])
			if err != nil {
				fmt.Println("Got error during sharing ", err)
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
	log.SetAllLoggers(logging.WARNING)
	log.SetLogLevel("node", "debug")
	log.SetLogLevel("sharingnode", "debug")
	log.SetLogLevel("autorelay", "debug")
	config, err := ParseFlags()
	if err != nil {
		panic(err)
	}

	ctx := context.Background()
	node := sharingnode.NewSharingNode(ctx, config)
	node.BootStrap()

	//select {}
	ScanInputCommands(node)
}
