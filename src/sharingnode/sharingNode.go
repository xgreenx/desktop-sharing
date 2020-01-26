package sharingnode

import "C"
import (
	"bufio"
	"context"
	"encoding/binary"
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
}

func NewSharingNode(ctx context.Context, config *config.SharingConfig) *SharingNode {
	return &SharingNode{
		node.NewNode(ctx, config.BootstrapConfig),
		config.SharingOptions,
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
}

func (n *SharingNode) ShareScreen(id peer.ID) error {
	if id == n.Host.ID() {
		return errors.New("can't share screen to self")
	}

	logger.Debug("Connecting to:", id)
	stream, err := n.Host.NewStream(n.Context, id, protocol.ID(config.StreamID))
	if err != nil {
		logger.Error(err)
		return err
	}
	event, err := n.Host.NewStream(n.Context, id, protocol.ID(config.EventID))
	if err != nil {
		logger.Error(err)
		return err
	}

	StartRemoteDesktop(stream, event)
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

	defer func() {
		f, _ := os.Create("StreamHeapW.out")
		pprof.WriteHeapProfile(f)
	}()

	//peerInfo, err := peer.AddrInfoFromP2pAddr(stream.Conn().RemoteMultiaddr())
	//if err != nil {
	//	panic(err)
	//}
	//n.Host.Connect(n.Context, *peerInfo)

	targetDisplay := 0
	StartScreenSharing(stream, targetDisplay)
	err := stream.Close()
	if err != nil {
		logger.Error(err)
	}
}

func (n *SharingNode) handleScreenEvent(stream network.Stream) {
	logger.Info("Got a new event connection!")

	defer func() {
		f, _ := os.Create("EventHeapR.out")
		pprof.WriteHeapProfile(f)
	}()

	targetDisplay := 0

	num := screenshot.NumActiveDisplays()

	offsetX := 0
	for i := targetDisplay + 1; i < num; i++ {
		offsetX = offsetX + screenshot.GetDisplayBounds(i).Dx()
	}

	receiver := NewEventReceiver(bufio.NewReader(stream), offsetX)
	receiver.Run()
	err := stream.Close()
	if err != nil {
		logger.Error(err)
	}
}

func writeInt(writer io.Writer, val int) error {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, uint64(val))
	_, err := writer.Write(b)
	if err != nil {
		return err
	}

	return nil
}

func readInt(reader io.Reader) (int, error) {
	b := make([]byte, 8)
	_, err := reader.Read(b)
	if err != nil {
		return 0, err
	}
	return int(binary.LittleEndian.Uint64(b)), nil
}

func StartScreenSharing(writer network.Stream, targetDisplay int) {
	logger.Info("Start sharing screen")
	streamCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bounds := screenshot.GetDisplayBounds(targetDisplay)
	w := bounds.Dx()
	h := bounds.Dy()
	err := writeInt(writer, w)
	if err != nil {
		logger.Error(err)
		return
	}
	err = writeInt(writer, h)
	if err != nil {
		logger.Error(err)
		return
	}

	dataCh := make(chan []byte, 4)
	go func() {
		var err error
		defer cancel()
		defer logger.Error(err)
		for data := range dataCh {
			b := make([]byte, 8)
			binary.LittleEndian.PutUint64(b, uint64(len(data)))

			_, err = writer.Write(append(b, data...))
			if err != nil {
				return
			}
		}
	}()
	StreamSend(streamCtx, w, h, dataCh)
	logger.Info("End sharing screen")
}

func StartRemoteDesktop(stream network.Stream, event network.Stream) {
	streamCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var err error
	width, err := readInt(stream)
	if err != nil {
		logger.Error(err)
		return
	}

	height, err := readInt(stream)
	if err != nil {
		logger.Error(err)
		return
	}

	myapp := app.New()
	win := myapp.NewWindow("Desktop Sharing")
	win.Resize(fyne.Size{width, height})

	imgWidget := canvas.NewImageFromImage(image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420))
	win.SetContent(imgWidget)

	eventSender := NewEventSender(bufio.NewWriter(event), width, height)
	eventSender.Subscribe(win)

	var closeCallback glfw.CloseCallback
	closeCallback = win.Viewport().SetCloseCallback(func(w *glfw.Window) {
		err = stream.Reset()
		if err != nil {
			logger.Error(err)
		}
		closeCallback(w)
	})

	senderChan := make(chan []byte)
	onImage := func(img *image.YCbCr) {
		imgWidget.Image = img
		c := win.Canvas()
		if c != nil {
			c.Refresh(imgWidget)
		}
	}
	go StreamReceive(streamCtx, width, height, senderChan, onImage)

	tmp := make([]byte, width*height*4)
	go func() {
		defer close(senderChan)
		for {
			frameSize, err := readInt(stream)
			if err != nil {
				logger.Error(err)
				cancel()
				return
			}

			off := 0
			for off < frameSize {
				n, err := stream.Read(tmp[off:frameSize])
				if err != nil {
					logger.Error(err)
					cancel()
					return
				}
				off += n
			}

			data := make([]byte, frameSize)
			copy(data, tmp[:frameSize])
			senderChan <- data
		}
	}()

	win.ShowAndRun()
}
