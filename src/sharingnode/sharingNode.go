package sharingnode

import "C"
import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
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
	"github.com/libp2p/go-libp2p-swarm"
	"github.com/multiformats/go-multiaddr"
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

func NewSharingNode(ctx context.Context, config *config.Config) *SharingNode {
	return &SharingNode{
		node.Node{
			ctx,
			config,
			nil,
			nil,
		},
	}
}

func (n *SharingNode) BootStrap() {
	n.Node.BootStrap()

	n.Node.Host.SetStreamHandler(protocol.ID(n.Config.ScreenProtocolId), handleScreenStream)
}

func (n *SharingNode) ShareScreen(id peer.ID) error {
	if id == n.Host.ID() {
		return errors.New("can't share screen to self")
	}

	logger.Debug("Connecting to:", id)
	stream, err := n.Host.NewStream(n.Context, id, protocol.ID(n.Config.ScreenProtocolId))

	// Let's try again via relay mechanism
	if err != nil {
		// Creates a relay address
		relayaddr, err := multiaddr.NewMultiaddr("/p2p-circuit/ipfs/" + id.Pretty())
		if err != nil {
			logger.Error(err)
			return err
		}

		// Since we just tried and failed to dial, the dialer system will, by default
		// prevent us from redialing again so quickly. Since we know what we're doing, we
		// can use this ugly hack (it's on our TODO list to make it a little cleaner)
		// to tell the dialer "no, its okay, let's try this again"
		n.Host.Network().(*swarm.Swarm).Backoff().Clear(id)

		h3relayInfo := peer.AddrInfo{
			ID:    id,
			Addrs: []multiaddr.Multiaddr{relayaddr},
		}
		if err := n.Host.Connect(context.Background(), h3relayInfo); err != nil {
			logger.Error(err)
			return err
		}

		stream, err = n.Host.NewStream(n.Context, id, protocol.ID(n.Config.ScreenProtocolId))
		if err != nil {
			logger.Error(err)
			return err
		}
	}

	StartScreenReceiving(stream)
	err = stream.Close()
	if err != nil {
		logger.Error(err)
	}

	defer func() {
		fmt.Println("Write to heap start receiving")
		f, _ := os.Create("heapR.out")
		err = pprof.WriteHeapProfile(f)
		fmt.Println("Write to heap end")
	}()

	return nil
}

func handleScreenStream(stream network.Stream) {
	logger.Info("Got a new sharing connection!")
	StartScreenSharing(stream)
	err := stream.Close()
	if err != nil {
		logger.Error(err)
	}

	defer func() {
		fmt.Println("Write to heap start sharing")
		f, _ := os.Create("heapS.out")
		err = pprof.WriteHeapProfile(f)
		fmt.Println("Write to heap end")
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

func StartScreenSharing(writer network.Stream) {
	logger.Info("Start sharing screen")
	streamCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reader := bufio.NewReader(writer)
	targetDisplay := 0

	n := screenshot.NumActiveDisplays()

	offsetX := 0
	for i := targetDisplay + 1; i < n; i++ {
		offsetX = offsetX + screenshot.GetDisplayBounds(i).Dx()
	}

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

	receiver := NewEventReceiver(reader, offsetX)
	go receiver.Run()

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

func StartScreenReceiving(stream network.Stream) {
	streamCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	writer := bufio.NewWriter(stream)
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

	eventSender := NewEventSender(writer, width, height)
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
			win.Canvas().Refresh(imgWidget)
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
