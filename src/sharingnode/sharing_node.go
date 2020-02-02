package sharingnode

import "C"
import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fyne.io/fyne"
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
	App           fyne.App
}

func NewSharingNode(ctx context.Context, config *config.SharingConfig, app fyne.App) *SharingNode {
	n := node.NewNode(ctx, config.BootstrapConfig)
	return &SharingNode{
		n,
		config.SharingOptions,
		nil,
		app,
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
	n.AccessVerifier = node.NewAccessVerifier(n.AccessStore, NewGUIAllower(n.Config, n.App), n.Host, n.Context, n.DataDht)
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

	if !result {
		goto Error
	} else {
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
		streamInfo := &StreamInfo{}

		err = write(stream, displaysInfo)
		if err != nil {
			goto Error
		}
		err = read(stream, streamInfo)
		if err != nil {
			goto Error
		}

		if uint32(len(displaysInfo.Displays)) <= streamInfo.ScreenOptions.TargetDisplay {
			streamInfo.ScreenOptions.TargetDisplay = uint32(len(displaysInfo.Displays)) - 1
		}

		client, err := n.StreamService.AddClient(stream, streamInfo, displaysInfo)
		if err != nil {
			goto Error
		}
		err = client.Start()
		if err != nil {
			goto Error
		}
	}

	return
Error:
	logger.Info("End sharing connection!")
	stream.Close()
	if err != nil {
		logger.Error(err)
	}
}

func (n *SharingNode) handleScreenEvent(stream network.Stream) {
	logger.Info("Got a new event connection!")
	defer func() {
		logger.Info("End event connection!")
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

	receiver := NewEventReceiver(stream, offsetX)
	receiver.Run()
}

func (n *SharingNode) ShareScreen(id peer.ID, targetDisplay int, control bool) error {
	if id == n.Host.ID() {
		return errors.New("can't share screen to self")
	}

	var err error
	defer func() {
		f, _ := os.Create("SharingHeap.out")
		err = pprof.WriteHeapProfile(f)
	}()

	logger.Debug("Connecting to:", id)
	screen, err := NewRemoteScreen(id, uint32(targetDisplay), n.SharingOptions, n.AccessVerifier)
	if err != nil {
		return err
	}

	if control {
		err = screen.AddControl()
		if err != nil {
			logger.Error(err)
		}
	}

	return screen.Show(n.App)
}

type RemoteScreen struct {
	config.SharingOptions
	Resolution     RecordResolution
	RemoteDisplay  DisplayInfo
	id             peer.ID
	targetDisplay  uint32
	accessVerifier *node.AccessVerifier
	stream         network.Stream
	event          network.Stream
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
	streamInfo.Resolution = options.Resolution
	streamInfo.StreamOptions.Options = options.StreamOptions
	streamInfo.ScreenOptions.GrabbingOptions = options.ScreenGrabbingOptions
	streamInfo.ScreenOptions.TargetDisplay = targetDisplay
	err = write(stream, streamInfo)
	if err != nil {
		return nil, err
	}

	remoteDisplay := displaysInfo.Displays[targetDisplay]
	r := &RemoteScreen{
		SharingOptions: *options,
		RemoteDisplay:  remoteDisplay,
		Resolution:     *NewRecordResolution(remoteDisplay.Width, options.Resolution),
		id:             id,
		targetDisplay:  targetDisplay,
		accessVerifier: verifier,
		stream:         stream,
	}

	return r, nil
}

func (r *RemoteScreen) AddControl() error {
	if r.event != nil {
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

	eventInfo := &EventInfo{}
	eventInfo.TargetDisplay = r.targetDisplay
	err = write(event, eventInfo)
	if err != nil {
		return err
	}
	r.event = event

	return nil
}

func (r *RemoteScreen) Show(app fyne.App) error {
	win := app.NewWindow("Remote Display")
	win.Resize(fyne.Size{r.Resolution.Width, r.Resolution.Height})

	imgWidget := canvas.NewImageFromImage(image.NewYCbCr(image.Rect(0, 0, r.Resolution.Width, r.Resolution.Height), image.YCbCrSubsampleRatio420))
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
	reader := NewDataReader(r.stream)
	streamErr := StreamReceive(reader, onImage)

	var eventSender *EventSender
	if r.event != nil {
		eventSender = NewEventSender(r.event, r.RemoteDisplay.Width, r.RemoteDisplay.Height)
		eventSender.Subscribe(win)
	}

	go func() {
		var err error
		var ok bool
		if eventSender != nil {
			go func() {
				err = <-eventSender.Error()
				logger.Warning(err)
			}()
		}
		select {
		case err, ok = <-streamErr:
		}

		if ok {
			logger.Info(err)
			win.Close()
			r.close()
		}
	}()

	win.Show()
	return nil
}

func (r *RemoteScreen) close() {
	err := r.stream.Reset()
	if err != nil {
		logger.Error(err)
	}
	if r.event != nil {
		err = r.event.Reset()
		if err != nil {
			logger.Error(err)
		}
	}
}
