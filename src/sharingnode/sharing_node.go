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
	displaysInfo := &DisplaysInfo{
		Displays: make([]DisplayInfo, num),
	}
	for i := 0; i < num; i++ {
		displaysInfo.Displays[i] = DisplayInfo{
			Width:  screenshot.GetDisplayBounds(i).Dx(),
			Height: screenshot.GetDisplayBounds(i).Dy(),
		}
	}

	err = write(stream, displaysInfo)
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

	if uint32(len(displaysInfo.Displays)) <= streamInfo.ScreenOptions.TargetDisplay {
		streamInfo.ScreenOptions.TargetDisplay = uint32(len(displaysInfo.Displays)) - 1
	}

	err = n.StreamService.AddClient(stream, streamInfo, displaysInfo)
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

	num := screenshot.NumActiveDisplays()
	displaysInfo := &DisplaysInfo{
		Displays: make([]DisplayInfo, num),
	}
	for i := 0; i < num; i++ {
		displaysInfo.Displays[i] = DisplayInfo{
			Width:  screenshot.GetDisplayBounds(i).Dx(),
			Height: screenshot.GetDisplayBounds(i).Dy(),
		}
	}

	err = write(stream, displaysInfo)
	if err != nil {
		logger.Error(err)
		return
	}

	eventInfo := &EventInfo{}
	err = read(stream, eventInfo)
	if err != nil {
		logger.Error(err)
		return
	}

	if uint32(len(displaysInfo.Displays)) <= eventInfo.TargetDisplay {
		eventInfo.TargetDisplay = uint32(len(displaysInfo.Displays)) - 1
	}

	offsetX := 0
	for i := uint32(0); i < eventInfo.TargetDisplay; i++ {
		offsetX = offsetX + screenshot.GetDisplayBounds(int(i)).Dx()
	}

	receiver := NewEventReceiver(bufio.NewReader(stream), offsetX)
	receiver.Run()
}

func (n *SharingNode) ShareScreen(id peer.ID) error {
	if id == n.Host.ID() {
		return errors.New("can't share screen to self")
	}

	var err error
	defer func() {
		f, _ := os.Create("SharingHeap.out")
		err = pprof.WriteHeapProfile(f)
	}()

	logger.Debug("Connecting to:", id)
	targetDisplay := uint32(0)
	control := true
	screen, err := NewRemoteScreen(id, targetDisplay, n.SharingOptions, n.AccessVerifier)
	if err != nil {
		return err
	}

	if control {
		err = screen.AddControl()
		if err != nil {
			return err
		}
	}

	return screen.ShowAndRun()
}

type RemoteScreen struct {
	config.SharingOptions
	Width          int
	Height         int
	id             peer.ID
	targetDisplay  uint32
	accessVerifier *node.AccessVerifier
	eventSender    *EventSender
	reader         *DataReader
}

func NewRemoteScreen(id peer.ID, targetDisplay uint32, options *config.SharingOptions, verifier *node.AccessVerifier) (*RemoteScreen, error) {
	stream, err := verifier.Access(id, protocol.ID(config.StreamID))
	if err != nil {
		return nil, err
	}

	displaysInfo := &DisplaysInfo{}
	err = read(stream, displaysInfo)
	if err != nil {
		return nil, err
	}

	if uint32(len(displaysInfo.Displays)) <= targetDisplay {
		targetDisplay = uint32(len(displaysInfo.Displays)) - 1
	}

	streamInfo := &StreamInfo{}
	streamInfo.StreamOptions.Options = options.StreamOptions
	streamInfo.ScreenOptions.GrabbingOptions = options.ScreenGrabbingOptions
	streamInfo.ScreenOptions.TargetDisplay = targetDisplay
	err = write(stream, streamInfo)
	if err != nil {
		return nil, err
	}

	r := &RemoteScreen{
		SharingOptions: *options,
		Width:          1280,
		Height:         720,
		id:             id,
		targetDisplay:  targetDisplay,
		accessVerifier: verifier,
	}

	r.reader = NewDataReader(stream)

	return r, nil
}

func (r *RemoteScreen) AddControl() error {
	if r.eventSender != nil {
		return nil
	}
	event, err := r.accessVerifier.Access(r.id, protocol.ID(config.EventID))
	if err != nil {
		logger.Error(err)
		return err
	}

	displaysInfo := &DisplaysInfo{}
	err = read(event, displaysInfo)
	if err != nil {
		return err
	}

	if uint32(len(displaysInfo.Displays)) <= r.targetDisplay {
		r.targetDisplay = uint32(len(displaysInfo.Displays)) - 1
	}
	remoteDisplay := displaysInfo.Displays[r.targetDisplay]

	eventInfo := &EventInfo{}
	eventInfo.TargetDisplay = r.targetDisplay
	err = write(event, eventInfo)
	if err != nil {
		return err
	}

	r.eventSender = NewEventSender(bufio.NewWriter(event), remoteDisplay.Width, remoteDisplay.Height)

	return nil
}

func (r *RemoteScreen) ShowAndRun() error {
	myapp := app.New()
	win := myapp.NewWindow("Desktop Sharing")
	win.Resize(fyne.Size{r.Width, r.Height})

	imgWidget := canvas.NewImageFromImage(image.NewYCbCr(image.Rect(0, 0, r.Width, r.Height), image.YCbCrSubsampleRatio420))
	win.SetContent(imgWidget)

	var closeCallback glfw.CloseCallback
	closeCallback = win.Viewport().SetCloseCallback(func(w *glfw.Window) {
		r.close()
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
	go StreamReceive(r.reader, onImage)

	if r.eventSender != nil {
		r.eventSender.Subscribe(win)
	}
	win.ShowAndRun()
	return nil
}

func (r *RemoteScreen) close() {
}
