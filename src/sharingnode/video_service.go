package sharingnode

import (
	"github.com/imkira/go-libav/avcodec"
	"github.com/imkira/go-libav/avformat"
	"github.com/imkira/go-libav/avutil"
	"github.com/imkira/go-libav/swscale"
	"github.com/pkg/errors"
	"sync"
)

type RecordResolution struct {
	Width  int
	Height int
}

func NewRecordResolution(width, height int) *RecordResolution {
	var defaultResolutions = []RecordResolution{
		{1920, 1080},
		{1920, 1440},
		{1280, 720},
		{854, 480},
		{640, 480},
	}

	i := 0
	for _, r := range defaultResolutions {
		i++
		if r.Width > width {
			continue
		}
		if r.Height > height {
			continue
		}
		break
	}

	result := defaultResolutions[i-1]
	return &result
}

type ImageProvider struct {
	sync.Mutex
	ScreenOptions
	Display         DisplayInfo
	Resolution      *RecordResolution
	swsContext      *swscale.Context
	avFormatContext *avformat.Context
	avInputFormat   *avformat.Input
	avCodecContext  *avcodec.Context
	encFrame        *avutil.Frame
	optionsScreen   *avutil.Dictionary
}

func (i *ImageProvider) Image(onImage func(*avutil.Frame) error) error {
	i.Lock()
	defer i.Unlock()

	if i.avFormatContext == nil {
		return errors.New("Image provider already closed")
	}

	decPacket, err := avcodec.NewPacket()
	if err != nil {
		return err
	}

	frameDone, err := i.avFormatContext.ReadFrame(decPacket)
	if err != nil {
		return err
	}

	if !frameDone {
		return err
	}

	_, err = i.avCodecContext.DecodeVideo(decPacket, func(decFrame *avutil.Frame) error {
		err = i.encFrame.MakeWritable()
		if err != nil {
			return err
		}

		i.swsContext.Scale(decFrame, 0, i.Display.Height, i.encFrame)
		err = onImage(i.encFrame)
		//decFrame.Unref()
		//decFrame.FreeData(0)
		return err
	})
	decPacket.Free()

	return err
}

func (i *ImageProvider) Close() {
	i.Lock()
	defer i.Unlock()
	if i.optionsScreen != nil {
		i.optionsScreen.Free()
		i.optionsScreen = nil
	}
	if i.avFormatContext != nil {
		i.avFormatContext.CloseInput()
		i.avFormatContext.Free()
		i.avFormatContext = nil
	}
	if i.swsContext != nil {
		i.swsContext.Free()
		i.swsContext = nil
	}
	if i.encFrame != nil {
		i.encFrame.Free()
		i.encFrame = nil
	}
}

type VideoEncoder struct {
	sync.Mutex
	StreamOptions
	codecContext *avcodec.Context
	encPacket    *avcodec.Packet
	codecOption  *avutil.Dictionary
}

func NewVideoEncoder(streamOptions *StreamOptions) *VideoEncoder {
	//avutil.SetLogLevel(avutil.LogLevelTrace)
	encoder := &VideoEncoder{
		StreamOptions: *streamOptions,
	}

	return encoder
}

func (e *VideoEncoder) Encode(provider *ImageProvider) (chan []byte, error) {
	e.Lock()
	defer e.Unlock()
	var err error

	ch := make(chan []byte, 4)
	e.codecOption = avutil.NewDictionary()
	codec := avcodec.FindEncoderByName("libx264")
	if codec == nil {
		err = errors.New("Codec not found")
		goto Error
	}

	e.codecContext, err = avcodec.NewContextWithCodec(codec)
	if err != nil {
		goto Error
	}

	e.encPacket, err = avcodec.NewPacket()
	if err != nil {
		goto Error
	}

	e.codecContext.SetWidth(provider.Resolution.Width)
	e.codecContext.SetHeight(provider.Resolution.Height)
	e.codecContext.SetPixelFormat(avutil.PIX_FMT_YUV420P)

	for key, value := range e.Options {
		err = e.codecOption.Set(key, value)
		if err != nil {
			goto Error
		}
	}

	err = e.codecContext.OpenWithCodec(codec, e.codecOption)
	if err != nil {
		goto Error
	}

	go func() {
		defer func() {
			close(ch)
			e.Close()
			provider.Close()
		}()
		index := 0
		for {
			e.Lock()
			if e.codecContext == nil {
				e.Unlock()
				return
			}
			//now := time.Now()
			err = provider.Image(func(encFrame *avutil.Frame) error {
				encFrame.SetPTS(int64(index))
				index++

				e.encPacket.SetData(nil)
				e.encPacket.SetSize(0)

				_, err = e.codecContext.EncodeVideo(e.encPacket, encFrame, func(data []byte) error {
					cData := make([]byte, len(data))
					copy(cData, data)
					ch <- cData
					return nil
				})

				return err
			})

			if err != nil {
				goto Error
			}
			if err != nil {
				goto Error
			}

			e.Unlock()
			continue
		Error:
			e.Unlock()
			logger.Error(err)
			return
		}
	}()

	return ch, nil

Error:
	e.Close()
	return nil, err
}

func (e *VideoEncoder) Close() {
	e.Lock()
	defer e.Unlock()
	if e.codecOption != nil {
		e.codecOption.Free()
		e.codecOption = nil
	}
	if e.codecContext != nil {
		e.codecContext.Free()
		e.codecContext = nil
	}
	if e.encPacket != nil {
		e.encPacket.Free()
		e.encPacket = nil
	}
}
