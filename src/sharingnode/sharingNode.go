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
	"github.com/BurntSushi/xgb"
	mshm "github.com/BurntSushi/xgb/shm"
	"github.com/BurntSushi/xgb/xinerama"
	"github.com/BurntSushi/xgb/xproto"
	"github.com/gen2brain/shm"
	"github.com/go-gl/glfw/v3.2/glfw"
	"github.com/ipfs/go-log"
	"github.com/kbinani/screenshot"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/protocol"
	"github.com/xgreenx/desktop-sharing/src/config"
	"github.com/xgreenx/desktop-sharing/src/node"
	"image"
	"image/color"
	"io"
	"os"
	"runtime/pprof"
	"time"
)

var logger = log.Logger("sharingnode")

type SharingNode struct {
	node.Node
}

func NewSharingNode(ctx context.Context, config *config.BootstrapConfig) *SharingNode {
	return &SharingNode{
		*node.NewNode(ctx, config),
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
		default:
			logger.Error("Unknown protocol", p)
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

	defer func() {
		f, _ := os.Create("StreamHeapW.out")
		err = pprof.WriteHeapProfile(f)
	}()
}

func (n *SharingNode) handleScreenEvent(stream network.Stream) {
	logger.Info("Got a new event connection!")

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

	defer func() {
		f, _ := os.Create("EventHeapR.out")
		err = pprof.WriteHeapProfile(f)
	}()
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

type screenShotTime struct {
	image *image.YCbCr
	time  time.Time
}

type dataTime struct {
	data []byte
	time time.Time
}

func GetScreenShots(streamContext context.Context, x, y, width, height int, ch chan *screenShotTime) error {
	c, err := xgb.NewConn()
	if err != nil {
		return err
	}
	defer c.Close()

	err = xinerama.Init(c)
	if err != nil {
		return err
	}

	reply, err := xinerama.QueryScreens(c).Reply()
	if err != nil {
		return err
	}

	primary := reply.ScreenInfo[0]
	x0 := int(primary.XOrg)
	y0 := int(primary.YOrg)

	useShm := true
	err = mshm.Init(c)
	if err != nil {
		useShm = false
	}

	screen := xproto.Setup(c).DefaultScreen(c)
	wholeScreenBounds := image.Rect(0, 0, int(screen.WidthInPixels), int(screen.HeightInPixels))
	targetBounds := image.Rect(x+x0, y+y0, x+x0+width, y+y0+height)
	intersect := wholeScreenBounds.Intersect(targetBounds)
	defer close(ch)
	for {
		select {
		case <-streamContext.Done():
			logger.Info("Stream context done")
			return nil
		default:
			err = func() error {
				var data []byte

				if useShm {
					shmSize := intersect.Dx() * intersect.Dy() * 4
					shmId, err := shm.Get(shm.IPC_PRIVATE, shmSize, shm.IPC_CREAT|0777)
					if err != nil {
						return err
					}

					seg, err := mshm.NewSegId(c)
					if err != nil {
						return err
					}

					data, err = shm.At(shmId, 0, 0)
					if err != nil {
						return err
					}

					mshm.Attach(c, seg, uint32(shmId), false)

					defer mshm.Detach(c, seg)
					defer shm.Rm(shmId)
					defer shm.Dt(data)

					_, err = mshm.GetImage(c, xproto.Drawable(screen.Root),
						int16(intersect.Min.X), int16(intersect.Min.Y),
						uint16(intersect.Dx()), uint16(intersect.Dy()), 0xffffffff,
						byte(xproto.ImageFormatZPixmap), seg, 0).Reply()
					if err != nil {
						return err
					}
				} else {
					xImg, err := xproto.GetImage(c, xproto.ImageFormatZPixmap, xproto.Drawable(screen.Root),
						int16(intersect.Min.X), int16(intersect.Min.Y),
						uint16(intersect.Dx()), uint16(intersect.Dy()), 0xffffffff).Reply()
					if err != nil {
						return err
					}

					data = xImg.Data
				}

				// BitBlt by hand

				img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio444)
				offset := 0
				for i := 0; i < width*height; i++ {
					r := data[offset+2]
					g := data[offset+1]
					b := data[offset]
					y, Cb, Cr := color.RGBToYCbCr(r, g, b)
					img.Y[i] = y
					img.Cb[i] = Cb
					img.Cr[i] = Cr
					offset += 4
				}

				sc := screenShotTime{img, time.Now()}

				ch <- &sc
				return nil
			}()
			if err != nil {
				logger.Error(err)
				return err
			}
		}
	}
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

	screenShotChan := make(chan *screenShotTime)
	go StreamSend(streamCtx, w, h, screenShotChan, func(data []byte) {
		err := writeInt(writer, len(data))
		if err != nil {
			logger.Error(err)
			cancel()
			return
		}

		_, err = writer.Write(data)

		if err != nil {
			logger.Error(err)
			cancel()
			return
		}
	})
	err = GetScreenShots(streamCtx, bounds.Min.X, bounds.Min.Y, w, h, screenShotChan)
	if err != nil {
		logger.Error(err)
	}
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

	imgWidget := canvas.NewImageFromImage(image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio444))
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

	senderChan := make(chan *dataTime)
	onImage := func(img *image.YCbCr) {
		myapp.Driver().RunOnMain(func() {
			imgWidget.Image = img
			c := win.Canvas()
			if c != nil {
				c.Refresh(imgWidget)
			}
		})
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
			senderChan <- &dataTime{data, time.Now()}
		}
	}()

	win.ShowAndRun()
}
