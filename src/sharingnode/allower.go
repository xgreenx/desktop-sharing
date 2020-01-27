package sharingnode

import (
	"fmt"
	"fyne.io/fyne"
	"fyne.io/fyne/app"
	"fyne.io/fyne/widget"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/protocol"
	"github.com/xgreenx/desktop-sharing/src/config"
	"github.com/xgreenx/desktop-sharing/src/node"
	"sync"
)

type GUIAllower struct {
	sync.Mutex
	node.ConnectionAllower
	bootstrap *config.BootstrapConfig
}

func NewGUIAllower(bootstrapConfig *config.BootstrapConfig) *GUIAllower {
	return &GUIAllower{
		bootstrap: bootstrapConfig,
	}
}

func getConnectionLabel(id protocol.ID, name string, peerID peer.ID) string {
	label := ""
	switch id {
	case config.StreamID:
		label = fmt.Sprintf("The remote node %s (%s) wants get stream from your screen. Do you allow it?", name, peerID)
	case config.EventID:
		label = fmt.Sprintf("The remote node %s (%s) wants send you mouse and key events. Do you allow it?", name, peerID)
	case config.CommandID:
		label = fmt.Sprintf("The remote node %s (%s) wants send you terminal commands. Do you allow it?", name, peerID)
	}

	return label
}

func getAccessLabel(id protocol.ID) string {
	label := ""
	switch id {
	case config.StreamID:
		label = fmt.Sprintf("Screen streaming")
	case config.EventID:
		label = fmt.Sprintf("Receiving events")
	case config.CommandID:
		label = fmt.Sprintf("Terminal commands")
	}

	return label
}

func (a GUIAllower) Allow(c *node.ConnectionInfo) (node.AllowResult, error) {
	a.Lock()
	defer a.Unlock()

	myapp := app.New()
	w := myapp.NewWindow("Allow access")

	result := node.NewAllowResult()

	pCBs := make([]fyne.CanvasObject, len(a.bootstrap.Protocols))
	for i, p := range a.bootstrap.Protocols {
		temp := p
		check := widget.NewCheck(getAccessLabel(p), func(b bool) {
			result.Protocols[temp] = b
		})
		check.Checked = c.Rights.IsAllowed(temp)
		pCBs[i] = check
	}

	hObjs := append([]fyne.CanvasObject{
		widget.NewLabel("Current access setup:"),
	}, pCBs...)

	okButton := widget.NewButton("Ok", func() {
		myapp.Quit()
	})
	okButton.Resize(fyne.NewSize(30, 100))

	objects := append([]fyne.CanvasObject{
		widget.NewLabel(getConnectionLabel(c.Protocol, c.Rights.Name(), c.Rights.Id())),
		widget.NewHBox(hObjs...),
		widget.NewCheck("Remember this result for future connections?", func(b bool) {
			result.Remember = b
		}),
		okButton,
	})

	w.SetContent(widget.NewVBox(
		objects...,
	))
	w.CenterOnScreen()

	w.ShowAndRun()

	return result, nil
}
