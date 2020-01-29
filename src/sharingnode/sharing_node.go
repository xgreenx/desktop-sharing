package sharingnode

import "C"
import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fyne.io/fyne"
	"fyne.io/fyne/app"
	"fyne.io/fyne/canvas"
	"github.com/go-gl/glfw/v3.2/glfw"
	"github.com/ipfs/go-log"
	"github.com/kbinani/screenshot"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/protocol"
	"github.com/xgreenx/desktop-sharing/src/config"
	"github.com/xgreenx/desktop-sharing/src/node"
	"image"
	"io"
	"os"
	"runtime/pprof"
)

var logger = log.Logger("sharingnode")

type SharingNode struct {
	*node.Node
	*config.SharingOptions
	StreamService *StreamService
}

func NewSharingNode(ctx context.Context, config *config.SharingConfig) *SharingNode {
	n := node.NewNode(ctx, config.BootstrapConfig)
	return &SharingNode{
		n,
		config.SharingOptions,
		nil,
	}
}

func (n *SharingNode) BootStrap() {
	n.Node.BootStrap()

	for _, p := range n.Config.Protocols {
		switch p {
		case config.StreamID:
			n.Node.Host.SetStreamHandler(protocol.ID(p), n.handleScreenStream)
		case config.EventID:
			n.Node.Host.SetStreamHandler(protocol.ID(p), n.handleScreenEvent)
		}
	}
	n.AccessVerifier = node.NewAccessVerifier(n.AccessStore, NewGUIAllower(n.Config), n.Host, n.Context, n.DataDht)
	n.StreamService = NewStreamService()
}

func (n *SharingNode) ShareScreen(id peer.ID) error {
	if id == n.Host.ID() {
		return errors.New("can't share screen to self")
	}

	logger.Debug("Connecting to:", id)
	stream, err := n.AccessVerifier.Access(id, protocol.ID(config.StreamID))
	if err != nil {
		logger.Error(err)
		return err
	}
	event, err := n.AccessVerifier.Access(id, protocol.ID(config.EventID))
	if err != nil {
		logger.Error(err)
		return err
	}

	StartRemoteDesktop(stream, event, *n.SharingOptions)
	err = stream.Close()
	if err != nil {
		logger.Error(err)
	}
	err = event.Close()
	if err != nil {
		logger.Error(err)
	}

	defer func() {
		f, _ := os.Create("SharingHeap.out")
		err = pprof.WriteHeapProfile(f)
	}()

	return nil
}

func (n *SharingNode) handleScreenStream(stream network.Stream) {
	logger.Info("Got a new sharing connection!")
	result, err := n.AccessVerifier.Verify(stream)
	if err != nil {
		logger.Warning(err)
	}
	if !result {
		return
	}

	num := screenshot.NumActiveDisplays()
	screenInfo := &ScreenInfo{
		Displays: make([]DisplayInfo, num),
	}
	for i := 0; i < num; i++ {
		screenInfo.Displays[i] = DisplayInfo{
			Width:  screenshot.GetDisplayBounds(i).Dx(),
			Height: screenshot.GetDisplayBounds(i).Dy(),
		}
	}

	err = write(stream, screenInfo)
	if err != nil {
		logger.Error(err)
		return
	}

	streamInfo := &StreamInfo{}
	err = read(stream, streamInfo)
	if err != nil {
		logger.Error(err)
		return
	}

	err = n.StreamService.AddClient(stream, streamInfo)
	if err != nil {
		logger.Error(err)
		return
	}
}

func (n *SharingNode) handleScreenEvent(stream network.Stream) {
	logger.Info("Got a new event connection!")
	defer func() {
		err := stream.Close()
		if err != nil {
			logger.Error(err)
		}
	}()

	defer func() {
		f, _ := os.Create("EventHeapR.out")
		pprof.WriteHeapProfile(f)
	}()

	result, err := n.AccessVerifier.Verify(stream)
	if err != nil {
		logger.Warning(err)
	}
	if !result {
		return
	}

	targetDisplay := 0

	num := screenshot.NumActiveDisplays()

	offsetX := 0
	for i := targetDisplay + 1; i < num; i++ {
		offsetX = offsetX + screenshot.GetDisplayBounds(i).Dx()
	}

	receiver := NewEventReceiver(bufio.NewReader(stream), offsetX)
	receiver.Run()
}

func write(writer io.Writer, val interface{}) error {
	b, err := json.Marshal(val)
	if err != nil {
		return err
	}

	_, err = writer.Write(append(b, '\n'))

	return err
}

func read(reader io.Reader, val interface{}) error {
	r := bufio.NewReader(reader)
	b, err := r.ReadBytes('\n')
	if err != nil {
		return err
	}

	return json.Unmarshal(b, val)
}

func StartRemoteDesktop(stream network.Stream, event network.Stream, options config.SharingOptions) {
	streamCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var err error
	screenInfo := &ScreenInfo{}
	err = read(stream, screenInfo)
	if err != nil {
		logger.Error(err)
		return
	}

	targetDisplay := 0
	remoteDisplay := screenInfo.Displays[targetDisplay]

	streamInfo := &StreamInfo{}
	streamInfo.StreamOptions.Options = options.StreamOptions
	streamInfo.ScreenOptions.GrabbingOptions = options.ScreenGrabbingOptions
	streamInfo.ScreenOptions.TargetDisplay = targetDisplay
	err = write(stream, streamInfo)
	if err != nil {
		logger.Error(err)
		return
	}

	myapp := app.New()
	win := myapp.NewWindow("Desktop Sharing")
	win.Resize(fyne.Size{remoteDisplay.Width, remoteDisplay.Height})

	imgWidget := canvas.NewImageFromImage(image.NewYCbCr(image.Rect(0, 0, remoteDisplay.Width, remoteDisplay.Height), image.YCbCrSubsampleRatio420))
	win.SetContent(imgWidget)

	eventSender := NewEventSender(bufio.NewWriter(event), remoteDisplay.Width, remoteDisplay.Height)
	eventSender.Subscribe(win)

	var closeCallback glfw.CloseCallback
	closeCallback = win.Viewport().SetCloseCallback(func(w *glfw.Window) {
		err = stream.Reset()
		if err != nil {
			logger.Error(err)
		}
		closeCallback(w)
	})

	onImage := func(img *image.YCbCr) error {
		imgWidget.Image = img
		c := win.Canvas()
		if c != nil {
			c.Refresh(imgWidget)
		}

		return nil
	}

	reader := NewDataReader(stream)

	go StreamReceive(streamCtx, remoteDisplay.Width, remoteDisplay.Height, reader, onImage)

	win.ShowAndRun()
}
