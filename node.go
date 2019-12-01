package desktop_sharing

import "C"
import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	myfyne "fyne.io/fyne"
	myapp "fyne.io/fyne/app"
	"fyne.io/fyne/canvas"
	"github.com/go-vgo/robotgo"
	"github.com/ipfs/go-log"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/protocol"
	discovery "github.com/libp2p/go-libp2p-discovery"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/multiformats/go-multiaddr"
	"github.com/pixiv/go-libjpeg/jpeg"
	hook "github.com/robotn/gohook"
	"image"
	"io"
	"sync"
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
	b, err := hex.DecodeString(n.config.PrivateKey)
	if err != nil {
		panic(err)
	}

	priv, _, err := crypto.GenerateEd25519Key(bytes.NewBuffer(b))
	if err != nil {
		panic(err)
	}

	n.host, err = libp2p.New(n.context,
		libp2p.ListenAddrs([]multiaddr.Multiaddr(n.config.ListenAddresses)...),
		libp2p.Identity(priv),
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

	StartScreenReceiving(stream, bufio.NewWriter(stream))

	return nil
}

func handleScreenStream(stream network.Stream) {
	logger.Info("Got a new sharing connection!")
	StartScreenSharing(stream, bufio.NewReader(stream))
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

func StartScreenSharing(writer io.Writer, reader *bufio.Reader) {
	w, h := robotgo.GetScreenSize()
	writeInt(writer, w)
	writeInt(writer, h)
	tmp := bytes.NewBuffer([]byte{})
	img := image.NewRGBA(image.Rect(0, 0, w, h))

	go func() {
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
			err := jpeg.Encode(tmp, img, &jpeg.EncoderOptions{Quality: 75})
			if err != nil {
				panic(err)
			}

			writeInt(writer, tmp.Len())
			_, err = tmp.WriteTo(writer)

			if err != nil {
				panic(err)
			}
		}
	}()

	if reader != nil {
		//go func() {
		for {
			b, err := reader.ReadBytes('\n')
			if err != nil {
				panic(err)
			}

			ev := hook.Event{}

			err = json.Unmarshal(b, &ev)
			if err != nil {
				panic(err)
			}

			fmt.Println(ev)
		}
		//}()
	}
}

func StartScreenReceiving(reader io.Reader, writer *bufio.Writer) {
	var err error
	width := readInt(reader)
	height := readInt(reader)
	tmp := make([]byte, width*height*4)
	//width, height := robotgo.GetScreenSize()
	app := myapp.New()

	win := app.NewWindow("Remote desktop")
	win.Resize(myfyne.Size{width, height})

	imgWidget := canvas.NewImageFromImage(image.NewRGBA(image.Rect(0, 0, width, height)))
	win.SetContent(imgWidget)

	go func() {
		for {
			frameSize := readInt(reader)

			off := 0
			for off < frameSize {
				n, err := reader.Read(tmp[off:frameSize])
				if err != nil {
					panic(err)
				}
				off += n
			}

			// Compress image for transporting
			imgWidget.Image, err = jpeg.Decode(bytes.NewBuffer(tmp[:frameSize]), &jpeg.DecoderOptions{})
			if err != nil {
				panic(err)
			}

			canvas.Refresh(imgWidget)
		}
	}()

	go func() {
		EvChan := robotgo.Start()
		defer robotgo.End()

		for ev := range EvChan {
			win.Canvas()
			if win.Content() == nil {
				continue
			}

			continue

			b, err := json.Marshal(&ev)
			if err != nil {
				panic(err)
			}

			_, err = writer.Write(b)
			if err != nil {
				panic(err)
			}

			err = writer.WriteByte('\n')
			if err != nil {
				panic(err)
			}
		}
	}()

	win.ShowAndRun()
}
