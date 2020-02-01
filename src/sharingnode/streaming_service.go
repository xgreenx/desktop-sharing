package sharingnode

// #include <stdlib.h>
// #include <stdint.h>
import "C"
import (
	"github.com/imkira/go-libav/avcodec"
	"github.com/imkira/go-libav/avutil"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/pkg/errors"
	"image"
	"sync"
)

type DisplayInfo struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type DisplaysInfo struct {
	Displays []DisplayInfo `json:"displays"`
}

type StreamOptions struct {
	Options map[string]string `json:"options"`
}

type ScreenOptions struct {
	GrabbingOptions map[string]string `json:"grabbing_options"`
	TargetDisplay   uint32            `json:"target_display"`
}

type StreamInfo struct {
	Resolution    int           `json:"resolution"`
	StreamOptions StreamOptions `json:"stream_options"`
	ScreenOptions ScreenOptions `json:"screen_options"`
}

type Client struct {
	sync.Mutex
	stream  network.Stream
	service *StreamService
	queue   *DataWriter
	Data    chan []byte
}

func NewClient(stream network.Stream, service *StreamService) *Client {
	return &Client{
		stream:  stream,
		service: service,
		queue:   NewDataWriter(stream),
		Data:    make(chan []byte, 128),
	}
}

func (c *Client) Start() error {
	for {
		select {
		case err := <-c.queue.Error:
			c.Close()
			return err
		case data, ok := <-c.Data:
			if !ok {
				return nil
			}
			c.queue.AddData(data)
		}
	}
}

func (c *Client) Close() {
	c.service.RemoveClient(c)
	close(c.Data)
	err := c.stream.Reset()
	if err != nil {
		logger.Error(err)
	}
}

type StreamSession struct {
	sync.Mutex
	clients      map[*Client]struct{}
	videoEncoder *VideoEncoder
	header       []byte
}

func NewStreamSession() *StreamSession {
	session := &StreamSession{
		clients: make(map[*Client]struct{}),
		header:  make([]byte, 0),
	}

	return session
}

func (s *StreamSession) Start(options *StreamInfo, displaysInfo *DisplaysInfo) error {
	provider, err := NewImageProvider(&options.ScreenOptions, displaysInfo, options.Resolution)
	if err != nil {
		return err
	}

	s.videoEncoder = NewVideoEncoder(&options.StreamOptions)
	ch, err := s.videoEncoder.Encode(provider)
	if err != nil {
		s.videoEncoder.Close()
		s.videoEncoder = nil
		return err
	}

	go s.processData(ch)

	return nil
}

func (s *StreamSession) processData(dataCh chan []byte) {
	s.Lock()
	s.header = <-dataCh

	for client, _ := range s.clients {
		// Non blocking sent
		select {
		case client.Data <- s.header:
		default:
		}
	}
	s.Unlock()
	for data := range dataCh {
		s.Lock()
		tmp := data
		for client, _ := range s.clients {
			// Non blocking sent
			select {
			case client.Data <- tmp:
			default:
			}
		}
		s.Unlock()
	}
	s.Lock()
	for client, _ := range s.clients {
		client.Close()
	}
	s.Unlock()
}

func (s *StreamSession) Active() bool {
	s.Lock()
	defer s.Unlock()
	return len(s.clients) != 0
}

func (s *StreamSession) AddClient(client *Client) {
	s.Lock()
	defer s.Unlock()
	s.clients[client] = struct{}{}
	client.Data <- s.header
}

func (s *StreamSession) RemoveClient(client *Client) {
	s.Lock()
	delete(s.clients, client)
	s.Unlock()

	if !s.Active() {
		s.videoEncoder.Close()
		s.videoEncoder = nil
	}
}

type StreamService struct {
	sync.Mutex
	ActiveSession *StreamSession
}

func NewStreamService() *StreamService {
	return &StreamService{}
}

func (s *StreamService) AddClient(stream network.Stream, info *StreamInfo, displaysInfo *DisplaysInfo) (*Client, error) {
	s.Lock()
	defer s.Unlock()
	//defer func() {
	//	err := stream.Close()
	//	if err != nil {
	//		logger.Error(err)
	//	}
	//}()
	//
	//defer func() {
	//	f, _ := os.Create("StreamHeapW.out")
	//	pprof.WriteHeapProfile(f)
	//}()

	var err error = nil
	if s.ActiveSession == nil {
		s.ActiveSession = NewStreamSession()

		err = s.ActiveSession.Start(info, displaysInfo)
		if err != nil {
			s.ActiveSession = nil
			return nil, err
		}
	}

	client := NewClient(stream, s)
	s.ActiveSession.AddClient(client)

	return client, nil
}

func (s *StreamService) RemoveClient(client *Client) {
	s.Lock()
	defer s.Unlock()

	s.ActiveSession.RemoveClient(client)
	if !s.ActiveSession.Active() {
		s.ActiveSession = nil
	}
}

func StreamReceive(reader *DataReader, onImage func(img *image.YCbCr) error) chan error {
	//avutil.SetLogLevel(avutil.LogLevelDebug)

	codec := avcodec.FindDecoderByName("h264")
	if codec == nil {
		panic(errors.New("Codec not found"))
	}
	codecContext, err := avcodec.NewContextWithCodec(codec)
	if err != nil {
		panic(err)
	}

	packet, err := avcodec.NewPacket()
	if err != nil {
		panic(err)
	}

	err = codecContext.OpenWithCodec(codec, nil)
	if err != nil {
		panic(err)
	}

	parserContext, err := avcodec.NewParserContext(codecContext)
	if err != nil {
		panic(err)
	}

	errCh := make(chan error, 1)

	go func() {
		defer close(errCh)
		defer packet.Free()
		defer codecContext.Free()

		needParse := true
		for {
			receipt, err := reader.GetData()
			if err != nil {
				errCh <- err
				return
			}

			data := receipt
			dataSize := len(data)

			for dataSize > 0 {
				if needParse {
					ret, err := parserContext.Parse(data, dataSize, packet)
					if err != nil {
						errCh <- err
						return
					}

					data = data[ret:]
					dataSize = dataSize - ret

					if packet.Size() == 0 {
						continue
					}
				} else {
					packet.SetData(data)
					packet.SetSize(dataSize)
					dataSize = 0
				}

				onFrame := func(f *avutil.Frame) error {
					ySize := f.Width() * f.Height()
					cdSize := f.Width() * f.Height() / 4
					crSize := f.Width() * f.Height() / 4
					yData := C.GoBytes(f.Data(0), C.int(ySize))
					cbData := C.GoBytes(f.Data(1), C.int(cdSize))
					crData := C.GoBytes(f.Data(2), C.int(crSize))
					img := image.NewYCbCr(image.Rect(0, 0, f.Width(), f.Height()), image.YCbCrSubsampleRatio420)
					for i := 0; i < ySize; i++ {
						img.Y[i] = yData[i]
						if i < cdSize {
							img.Cb[i] = cbData[i]
						}
						if i < crSize {
							img.Cr[i] = crData[i]
						}
					}

					return onImage(img)
				}

				_, err = codecContext.DecodeVideo(packet, onFrame)

				if err != nil {
					errCh <- err
					return
				}
				if needParse {
					parserContext.Free()
					needParse = false
				} else {
					C.free(packet.Data())
				}
			}
		}
	}()

	return errCh
}
