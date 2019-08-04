package main

import "C"
import (
	"bufio"
	"context"
	"fmt"
	"github.com/go-vgo/robotgo"
	"github.com/ipfs/go-log"
	"github.com/whyrusleeping/go-logging"
	sharing "github.com/xgreenx/desktop-sharing"
	"os"
	"strings"
)

//
//func readData(rw *bufio.ReadWriter) {
//	for {
//		str, err := rw.ReadString('\n')
//		if err != nil {
//			fmt.Println("Error reading from buffer")
//			panic(err)
//		}
//
//		if str == "" {
//			return
//		}
//		if str != "\n" {
//			// Green console colour: 	\x1b[32m
//			// Reset console colour: 	\x1b[0m
//			fmt.Printf("\x1b[32m%s\x1b[0m> ", str)
//		}
//
//	}
//}

func ScanInputCommands(node *sharing.Node) {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		arg := strings.Split(scanner.Text(), " ")

		switch arg[0] {
		case "list":
			fmt.Println(node.List())
		case "screen":
			err := node.ShareScreen(arg[1])
			if err != nil {
				fmt.Println("Got error during sharing ", err)
			}
		default:
			fmt.Println("Unknown command ", arg[0])
		}
	}
}

func main() {
	EvChan := robotgo.Start()
	defer robotgo.End()

	for ev := range EvChan {
		fmt.Println("hook: ", ev)
	}
}

func main2() {
	log.SetAllLoggers(logging.WARNING)
	log.SetLogLevel("node", "debug")
	config, err := ParseFlags()
	if err != nil {
		panic(err)
	}

	ctx := context.Background()

	node := sharing.NewNode(ctx, config)
	node.BootStrap()

	if len(config.BootstrapPeers) != 0 {
		//ScanInputCommands(node)
		node.ShareScreen("")
	} else {
		select {} //ScanInputCommands(node)
	}
}
