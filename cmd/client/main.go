package main

import "C"
import (
	"context"
	"fmt"
	"fyne.io/fyne"
	"fyne.io/fyne/app"
	"fyne.io/fyne/layout"
	"fyne.io/fyne/theme"
	"fyne.io/fyne/widget"
	"github.com/ipfs/go-log"
	"github.com/xgreenx/desktop-sharing/src/config"
	"github.com/xgreenx/desktop-sharing/src/sharingnode"
	"os"
)

//
//func ScanInputCommands(node *sharingnode.SharingNode) {
//	scanner := bufio.NewScanner(os.Stdin)
//	for scanner.Scan() {
//		arg := strings.Split(scanner.Text(), " ")
//
//		switch arg[0] {
//		case "list":
//			node.PrintList()
//		case "screen":
//			if len(arg) < 2 {
//				fmt.Println("Missed node id")
//				continue
//			}
//
//			id, err := peer.IDB58Decode(arg[1])
//			if err != nil || id == "" {
//				fmt.Println("Wrong id of node ", err)
//				continue
//			}
//
//			targetDisplay := 0
//
//			if len(arg) > 2 {
//				t, err := strconv.ParseUint(arg[2], 10, 0)
//				if err != nil {
//					fmt.Println(fmt.Sprintf("Wrong target display %s, will use targetDisplay = %d", err.Error(), targetDisplay))
//				} else {
//					targetDisplay = int(t)
//				}
//			}
//
//			control := true
//			if len(arg) > 3 {
//				c, err := strconv.ParseBool(arg[3])
//				if err != nil {
//					fmt.Println(fmt.Sprintf("Wrong control varibale %s, will use control = %s", err.Error(), control))
//				} else {
//					control = c
//				}
//			}
//
//			err = node.ShareScreen(id, targetDisplay, control)
//			if err != nil {
//				fmt.Println("Got error during sharing ", err)
//				continue
//			}
//		default:
//			fmt.Println("Unknown command ", arg[0])
//		}
//	}
//}

type SizeLayout struct {
}

func (s *SizeLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for _, child := range objects {
		child.Resize(size)
	}
}

func (s *SizeLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	size := fyne.NewSize(0, 0)
	for _, child := range objects {
		if !child.Visible() {
			continue
		}

		size = size.Union(child.Size())
	}

	return size
}

func main() {
	myapp := app.New()
	conf := config.NewSharingConfig(config.NewBootstrapConfig())

	err := conf.LoadConfig()
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
	node := sharingnode.NewSharingNode(ctx, conf, myapp)
	node.BootStrap()

	myName, err := os.Hostname()
	if err != nil {
		panic(err)
	}

	fmt.Println("Starting GUI")
	w := myapp.NewWindow("Desktop sharing")
	w.Resize(fyne.NewSize(400, 700))
	w.SetFixedSize(true)

	var allElements fyne.CanvasObject
	nodeList := widget.NewVBox()
	scrollContainer := widget.NewScrollContainer(nodeList)

	var refreshBt *widget.Button
	refreshBt = widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), func() {
		refreshBt.Disable()
		chNodes := node.NodeList()
		nodeList.Children = []fyne.CanvasObject{}
		w.Canvas().Refresh(nodeList)

		for nodeInfo := range chNodes {
			tmp := nodeInfo
			pingWrapper := fyne.NewContainer(widget.NewLabel(fmt.Sprintf("%d", nodeInfo.Ping)))
			pingWrapper.Layout = &SizeLayout{}
			pingWrapper.Resize(fyne.NewSize(50, pingWrapper.MinSize().Height))
			newNode := fyne.NewContainerWithLayout(layout.NewGridLayout(2),
				widget.NewHBox(
					pingWrapper,
					widget.NewLabel(nodeInfo.Name),
				),
				widget.NewButton(nodeInfo.Id.Pretty(), func() {
					if refreshBt.Disabled() {
						return
					}

					err = node.ShareScreen(tmp.Id, 0, true)
					if err != nil {
						fmt.Println(err)
					}
				}),
			)

			nodeList.Append(newNode)
			w.Canvas().Refresh(nodeList)
			scrollContainer.Resize(fyne.NewSize(scrollContainer.Size().Width, w.Size().Height-allElements.MinSize().Height))
		}
		refreshBt.Enable()
	})
	refreshBt.Resize(fyne.NewSize(30, 30))
	buttonMenu := widget.NewHBox(
		refreshBt,
	)

	myInfo := fyne.NewContainerWithLayout(layout.NewGridLayout(2),
		widget.NewLabel(fmt.Sprintf("My info: %s", myName)),
		widget.NewLabel(node.Host.ID().Pretty()),
	)

	pingWrapper := fyne.NewContainer(widget.NewLabel("Ping"))
	pingWrapper.Layout = &SizeLayout{}
	pingWrapper.Resize(fyne.NewSize(50, pingWrapper.MinSize().Height))
	nodeHeader := fyne.NewContainerWithLayout(layout.NewGridLayout(2),
		widget.NewHBox(
			pingWrapper,
			widget.NewLabel("Name"),
		),
		widget.NewLabel("Node Id"),
	)

	nodes := widget.NewVBox(
		myInfo,
		nodeHeader,
		scrollContainer,
	)
	allElements = fyne.NewContainerWithLayout(layout.NewMaxLayout(),
		widget.NewVBox(
			buttonMenu,
			nodes,
		),
	)

	w.SetContent(allElements)
	w.CenterOnScreen()

	w.ShowAndRun()
	//
	//peerId, _ := peer.Decode("12D3KooWEj6GxaVrmKWEciRjQkBfEPvTqMyNxtBmmzvnNkavCo18")
	//node.ShareScreen(peerId)
	//
	//select {}
}
