package sharingnode

import "C"
import (
	"bufio"
	"bytes"
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
	"github.com/pixiv/go-libjpeg/jpeg"
	"github.com/xgreenx/desktop-sharing/src/config"
	"github.com/xgreenx/desktop-sharing/src/node"
	"image"
	"image/color"
	"io"
	"time"
)

var myapp fyne.App
var logger = log.Logger("sharingnode")

func init() {
	myapp = app.New()
	win := myapp.NewWindow("Desktop Sharing")
	win.Close()
	go myapp.Driver().Run()
}

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

	return nil
}

func handleScreenStream(stream network.Stream) {
	logger.Info("Got a new sharing connection!")
	StartScreenSharing(stream)
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

type dataTime struct {
	buffer *bytes.Buffer
	time   time.Time
}

func GetScreenShots(x, y, width, height int, ch chan *dataTime) error {
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

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for {
		now := time.Now()

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
			offset := 0
			for iy := intersect.Min.Y; iy < intersect.Max.Y; iy++ {
				for ix := intersect.Min.X; ix < intersect.Max.X; ix++ {
					r := data[offset+2]
					g := data[offset+1]
					b := data[offset]
					img.SetRGBA(ix-(x+x0), iy-(y+y0), color.RGBA{r, g, b, 255})
					offset += 4
				}
			}

			tmp := bytes.NewBuffer([]byte{})
			// Compress image for transporting
			err = jpeg.Encode(tmp, img, &jpeg.EncoderOptions{Quality: 75})
			if err != nil {
				return err
			}

			fmt.Println(time.Now().Sub(now))

			ch <- &dataTime{tmp, time.Now()}
			return nil
		}()
		if err != nil {
			logger.Error(err)
			return err
		}
	}

	return nil
}

func StartScreenSharing(writer network.Stream) {
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

	if reader != nil {
		receiver := NewEventReceiver(reader, offsetX)
		go func() {
			receiver.Run()
		}()
	}

	senderChan := make(chan *dataTime)

	go func() {
		lastBytes := 0
		last := time.Now()
		for data := range senderChan {
			err := func() error {
				defer func() {
					last = data.time
					lastBytes = data.buffer.Len()
					data.buffer.Reset()
				}()

				if lastBytes == data.buffer.Len() {
					return nil
				}

				t := time.Now().Sub(last).Milliseconds()
				if t > 150 {
					fmt.Println("Skip frame because delay is", t)
					return nil
				}

				err := writeInt(writer, data.buffer.Len())
				if err != nil {
					logger.Error(err)
					return err
				}

				_, err = data.buffer.WriteTo(writer)

				if err != nil {
					logger.Error(err)
					return err
				}

				return nil
			}()

			if err != nil {
				break
			}
		}
		close(senderChan)
	}()

	defer func() {
		//fmt.Println("Write to heap start")
		//f, _ := os.Create("heap.out")
		//err = pprof.WriteHeapProfile(f)
		//fmt.Println("Write to heap end")
		recover()
	}()
	err = GetScreenShots(bounds.Min.X, bounds.Min.Y, w, h, senderChan)
	if err != nil {
		logger.Error(err)
	}
}

func StartScreenReceiving(stream network.Stream) {
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
	tmp := make([]byte, width*height*4)
	//width, height := robotgo.GetScreenSize()

	logger.Info("Create new window")
	win := myapp.NewWindow("Desktop Sharing")
	logger.Info("Resize new window")
	win.Resize(fyne.Size{width, height})

	imgWidget := canvas.NewImageFromImage(image.NewRGBA(image.Rect(0, 0, width, height)))
	logger.Info("SetContent new window")
	win.SetContent(imgWidget)

	eventSender := NewEventSender(writer, width, height)
	eventSender.Subscribe(win)

	win.Show()
	win.Viewport().SetCloseCallback(func(w *glfw.Window) {
		err = stream.Reset()
		if err != nil {
			logger.Error(err)
		}
		go win.Close()
	})
	ok := true
	for ok {
		frameSize, err := readInt(stream)
		if err != nil {
			logger.Error(err)
			break
		}

		off := 0
		for off < frameSize {
			n, err := stream.Read(tmp[off:frameSize])
			if err != nil {
				logger.Error(err)
				ok = false
				break
			}
			off += n
		}

		// Compress image for transporting
		imgWidget.Image, err = jpeg.Decode(bytes.NewBuffer(tmp[:frameSize]), &jpeg.DecoderOptions{})
		if err != nil {
			logger.Error(err)
			break
		}

		canvas.Refresh(imgWidget)
	}

	win.Close()
}
