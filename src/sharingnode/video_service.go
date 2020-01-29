package sharingnode

import (
	"fmt"
	"github.com/imkira/go-libav/avcodec"
	"github.com/imkira/go-libav/avformat"
	"github.com/imkira/go-libav/avutil"
	"github.com/imkira/go-libav/swscale"
	"github.com/pkg/errors"
	"sync"
)

type ImageProvider struct {
	sync.Mutex
	ScreenOptions
	DisplayInfo
	swsContext      *swscale.Context
	avFormatContext *avformat.Context
	avInputFormat   *avformat.Input
	avCodecContext  *avcodec.Context
	decPacket       *avcodec.Packet
	encFrame        *avutil.Frame
	optionsScreen   *avutil.Dictionary
}

func NewImageProvider(options *ScreenOptions, displaysInfo *DisplaysInfo) (*ImageProvider, error) {
	provider := &ImageProvider{
		ScreenOptions: *options,
		DisplayInfo:   displaysInfo.Displays[int(options.TargetDisplay)],
	}

	offsetX := 0
	for i := int(options.TargetDisplay); i < len(displaysInfo.Displays); i++ {
		offsetX += displaysInfo.Displays[i].Width
	}

	provider.optionsScreen = avutil.NewDictionary()

	var err error
	provider.swsContext, err = swscale.NewContext(
		&swscale.DataDescription{provider.Width, provider.Height, avutil.PIX_FMT_RGBA},
		&swscale.DataDescription{provider.Width, provider.Height, avutil.PIX_FMT_YUV420P},
	)
	if err != nil {
		goto Error
	}

	provider.avFormatContext, err = avformat.NewContextForInput()
	if err != nil {
		goto Error
	}

	// TODO: support macOS and window
	provider.avInputFormat = avformat.FindInputByShortName("x11grab")
	if provider.avInputFormat == nil {
		err = errors.New("pAVInputFormat is nil")
		goto Error
	}

	provider.ScreenOptions.GrabbingOptions["video_size"] = fmt.Sprintf("%dx%d", provider.Width, provider.Height)
	for key, value := range provider.ScreenOptions.GrabbingOptions {
		err = provider.optionsScreen.Set(key, value)
		if err != nil {
			goto Error
		}
	}

	err = provider.avFormatContext.OpenInput(fmt.Sprintf(":0.0+%d,0", offsetX), provider.avInputFormat, provider.optionsScreen)
	if err != nil {
		goto Error
	}

	err = provider.avFormatContext.FindStreamInfo([]*avutil.Dictionary{provider.optionsScreen})
	if err != nil {
		goto Error
	}

	for _, s := range provider.avFormatContext.Streams() {
		provider.avCodecContext = s.CodecContext()
		if provider.avCodecContext.CodecType() == avutil.MediaTypeVideo {
			break
		}
	}

	err = provider.avCodecContext.OpenWithCodec(avcodec.FindDecoderByID(provider.avCodecContext.CodecID()), nil)
	if err != nil {
		goto Error
	}

	provider.decPacket, err = avcodec.NewPacket()
	if err != nil {
		goto Error
	}

	provider.encFrame, err = avutil.NewFrame()
	if err != nil {
		goto Error
	}

	provider.encFrame.SetWidth(provider.Width)
	provider.encFrame.SetHeight(provider.Height)
	provider.encFrame.SetPixelFormat(avutil.PIX_FMT_YUV420P)

	err = provider.encFrame.GetBuffer()
	if err != nil {
		goto Error
	}

	return provider, nil

Error:
	provider.Close()
	return nil, err
}

func (i *ImageProvider) Image(onImage func(*avutil.Frame) error) error {
	i.Lock()
	defer i.Unlock()

	if i.avFormatContext == nil {
		return errors.New("Image provider already closed")
	}

	frameDone, err := i.avFormatContext.ReadFrame(i.decPacket)
	if err != nil {
		return err
	}

	if !frameDone {
		return err
	}

	_, err = i.avCodecContext.DecodeVideo(i.decPacket, func(decFrame *avutil.Frame) error {
		err = i.encFrame.MakeWritable()
		if err != nil {
			return err
		}

		i.swsContext.Scale(decFrame, 0, i.Height, i.encFrame)
		err = onImage(i.encFrame)
		decFrame.FreeData(0)
		return err
	})

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

	e.codecContext.SetBitRate(90000)
	e.codecContext.SetWidth(provider.Width)
	e.codecContext.SetHeight(provider.Height)
	e.codecContext.SetTimeBase(avutil.NewRational(1, 10))
	e.codecContext.SetFrameRate(avutil.NewRational(10, 1))
	e.codecContext.SetMaxBFrames(0)
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
