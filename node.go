package desktop_sharing

import "C"
import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"fyne.io/fyne"
	"fyne.io/fyne/app"
	"fyne.io/fyne/canvas"
	"github.com/go-vgo/robotgo"
	"github.com/ipfs/go-log"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/protocol"
	discovery "github.com/libp2p/go-libp2p-discovery"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/multiformats/go-multiaddr"
	"github.com/pixiv/go-libjpeg/jpeg"
	"image"
	"io"
	"sync"
	"time"
	"unsafe"
)

const NODES_TAG = "nodes"

var logger = log.Logger("node")

type Node struct {
	context   context.Context
	config    *Config
	dht       *dht.IpfsDHT
	discovery *discovery.RoutingDiscovery
	host      host.Host
}

func NewNode(ctx context.Context, config *Config) *Node {
	return &Node{
		ctx,
		config,
		nil,
		nil,
		nil,
	}
}

func (n *Node) BootStrap() {
	var err error
	n.host, err = libp2p.New(n.context,
		libp2p.ListenAddrs([]multiaddr.Multiaddr(n.config.ListenAddresses)...),
	)
	if err != nil {
		panic(err)
	}
	logger.Info("Host created. We are:", n.host.ID())
	logger.Info(n.host.Addrs())

	n.host.SetStreamHandler(protocol.ID(n.config.ScreenProtocolId), handleScreenStream)

	n.dht, err = dht.New(n.context, n.host)
	if err != nil {
		panic(err)
	}

	logger.Debug("Bootstrapping the DHT")
	if err = n.dht.Bootstrap(n.context); err != nil {
		panic(err)
	}

	var wg sync.WaitGroup
	for _, peerAddr := range n.config.BootstrapPeers {
		peerinfo, err := peer.AddrInfoFromP2pAddr(peerAddr)

		if err != nil {
			panic(err)
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := n.host.Connect(n.context, *peerinfo); err != nil {
				logger.Warning(err)
			} else {
				logger.Info("Connection established with bootstrap node:", *peerinfo)
			}
		}()
	}
	wg.Wait()

	logger.Info("Announcing ourselves...")
	n.discovery = discovery.NewRoutingDiscovery(n.dht)
	discovery.Advertise(n.context, n.discovery, NODES_TAG)
	logger.Debug("Successfully announced!")
}

func (n *Node) List() peer.AddrInfo {
	logger.Debug("Searching for other peers...")
	peerChan, err := n.discovery.FindPeers(n.context, NODES_TAG)
	if err != nil {
		panic(err)
	}

	return <-peerChan
}

func (n *Node) ShareScreen(addr string) error {
	peerAddr := n.config.BootstrapPeers[0]
	//peerAddr, err := maddr.NewMultiaddr(addr)
	//if err != nil {
	//	return err
	//}

	peerInfo, err := peer.AddrInfoFromP2pAddr(peerAddr)
	if err != nil {
		return err
	}

	if peerInfo.ID == n.host.ID() {
		return errors.New("can't share screen to self")
	}

	logger.Debug("Connecting to:", peerInfo)
	stream, err := n.host.NewStream(n.context, peerInfo.ID, protocol.ID(n.config.ScreenProtocolId))
	if err != nil {
		return err
	}

	StartScreenReceiving(stream)

	return nil
}

func handleScreenStream(stream network.Stream) {
	logger.Info("Got a new sharing connection!")
	StartScreenSharing(stream)
}

func writeInt(writer io.Writer, val int) {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, uint64(val))
	_, err := writer.Write(b)
	if err != nil {
		panic(err)
	}
}

func readInt(reader io.Reader) int {
	b := make([]byte, 8)
	_, err := reader.Read(b)
	if err != nil {
		panic(err)
	}
	return int(binary.LittleEndian.Uint64(b))
}

func StartScreenSharing(writer io.Writer) {
	w, h := robotgo.GetScreenSize()
	writeInt(writer, w)
	writeInt(writer, h)
	tmp := bytes.NewBuffer([]byte{})

	img := image.NewRGBA(image.Rect(0, 0, w, h))

	for {
		bitmapRef := robotgo.CaptureScreen(0, 0, w, h)
		bitmap := robotgo.ToBitmap(bitmapRef)
		gbytes := C.GoBytes(unsafe.Pointer(bitmap.ImgBuf), (C.int)(bitmap.Height*bitmap.Bytewidth))

		for i := 0; i < len(gbytes); i = i + 4 {
			img.Pix[i+0] = gbytes[i+2]
			img.Pix[i+1] = gbytes[i+1]
			img.Pix[i+2] = gbytes[i+0]
			img.Pix[i+3] = gbytes[i+3]
		}
		robotgo.FreeBitmap(bitmapRef)

		// Compress image for transporting
		start := time.Now()
		err := jpeg.Encode(tmp, img, &jpeg.EncoderOptions{Quality: 75})
		fmt.Print(time.Since(start))
		if err != nil {
			panic(err)
		}

		fmt.Println("FrameSize send ", tmp.Len())
		writeInt(writer, tmp.Len())
		_, err = tmp.WriteTo(writer)

		if err != nil {
			panic(err)
		}
	}
}

func StartScreenReceiving(reader io.Reader) {
	var err error
	width := readInt(reader)
	height := readInt(reader)
	tmp := make([]byte, width*height*4)
	//width, height := robotgo.GetScreenSize()
	app := app.New()

	win := app.NewWindow("Remote desktop")
	win.Resize(fyne.Size{width, height})

	imgWidget := canvas.NewImageFromImage(image.NewRGBA(image.Rect(0, 0, width, height)))
	win.SetContent(imgWidget)

	go func() {
		for {
			frameSize := readInt(reader)

			off := 0
			for off < frameSize {
				fmt.Println("FrameSize got ", frameSize)
				n, err := reader.Read(tmp[off:frameSize])
				if err != nil {
					panic(err)
				}
				fmt.Println("Got n = ", n)
				off += n
			}

			// Compress image for transporting
			start := time.Now()
			imgWidget.Image, err = jpeg.Decode(bytes.NewBuffer(tmp[:frameSize]), &jpeg.DecoderOptions{})
			fmt.Println(time.Since(start))

			if err != nil {
				panic(err)
			}

			canvas.Refresh(imgWidget)
			fmt.Println("Hello world")
		}
	}()

	win.ShowAndRun()
}
