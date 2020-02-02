package sharingnode

import (
	"errors"
	"fmt"
	"github.com/imkira/go-libav/avcodec"
	"github.com/imkira/go-libav/avformat"
	"github.com/imkira/go-libav/avutil"
	"github.com/imkira/go-libav/swscale"
)

func NewImageProvider(options *ScreenOptions, displaysInfo *DisplaysInfo, resolution int) (*ImageProvider, error) {
	display := displaysInfo.Displays[int(options.TargetDisplay)]
	provider := &ImageProvider{
		ScreenOptions: *options,
		Resolution:    NewRecordResolution(display.Width, resolution),
		Display:       display,
	}
	offsetX := 0
	for i := 0; i < int(options.TargetDisplay); i++ {
		offsetX += displaysInfo.Displays[i].Width
	}

	provider.optionsScreen = avutil.NewDictionary()

	var err error
	provider.swsContext, err = swscale.NewContext(
		&swscale.DataDescription{provider.Display.Width, provider.Display.Height, avutil.PIX_FMT_RGBA},
		&swscale.DataDescription{provider.Resolution.Width, provider.Resolution.Height, avutil.PIX_FMT_YUV420P},
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

	provider.ScreenOptions.GrabbingOptions["video_size"] = fmt.Sprintf("%dx%d", provider.Display.Width, provider.Display.Height)
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

	provider.encFrame, err = avutil.NewFrame()
	if err != nil {
		goto Error
	}

	provider.encFrame.SetWidth(provider.Resolution.Width)
	provider.encFrame.SetHeight(provider.Resolution.Height)
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
