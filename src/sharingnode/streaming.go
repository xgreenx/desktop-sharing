package sharingnode

// #include <stdlib.h>
// #include <stdint.h>
import "C"
import (
	"context"
	"fmt"
	"github.com/imkira/go-libav/avcodec"
	"github.com/imkira/go-libav/avformat"
	"github.com/imkira/go-libav/avutil"
	"github.com/imkira/go-libav/swscale"
	"github.com/pkg/errors"
	"image"
)

func StreamSend(streamContext context.Context, width, height int, dataCh chan []byte) {
	//avutil.SetLogLevel(avutil.LogLevelTrace)

	codec := avcodec.FindEncoderByName("libx264")
	if codec == nil {
		panic(errors.New("Codec not found"))
	}
	codecContext, err := avcodec.NewContextWithCodec(codec)
	if err != nil {
		panic(err)
	}

	encFrame, err := avutil.NewFrame()
	if err != nil {
		panic(err)
	}

	encPacket, err := avcodec.NewPacket()
	if err != nil {
		panic(err)
	}

	codecContext.SetBitRate(90000)
	codecContext.SetWidth(width)
	codecContext.SetHeight(height)
	codecContext.SetTimeBase(avutil.NewRational(1, 10))
	codecContext.SetFrameRate(avutil.NewRational(10, 1))
	codecContext.SetMaxBFrames(0)
	codecContext.SetPixelFormat(avutil.PIX_FMT_YUV420P)

	options := avutil.NewDictionary()
	err = options.Set("preset", "ultrafast")
	if err != nil {
		panic(err)
	}
	err = options.Set("crf", "37")
	if err != nil {
		panic(err)
	}
	err = options.Set("ar", "44100")
	if err != nil {
		panic(err)
	}
	err = options.Set("r", "10")
	if err != nil {
		panic(err)
	}
	err = options.Set("ac", "2")
	if err != nil {
		panic(err)
	}
	err = options.Set("tune", "zerolatency")
	if err != nil {
		panic(err)
	}

	err = options.Set("probesize", "32")
	if err != nil {
		panic(err)
	}

	err = options.Set("maxrate", "750k")
	if err != nil {
		panic(err)
	}

	err = options.Set("bufsize", "3000k")
	if err != nil {
		panic(err)
	}

	err = codecContext.OpenWithCodec(codec, options)
	if err != nil {
		panic(err)
	}

	encFrame.SetWidth(codecContext.Width())
	encFrame.SetHeight(codecContext.Height())
	encFrame.SetPixelFormat(codecContext.PixelFormat())

	err = encFrame.GetBuffer()
	if err != nil {
		panic(err)
	}

	swsContext, err := swscale.NewContext(
		&swscale.DataDescription{width, height, avutil.PIX_FMT_RGBA},
		&swscale.DataDescription{width, height, avutil.PIX_FMT_YUV420P},
	)
	if err != nil {
		panic(err)
	}

	pAVFormatContext, err := avformat.NewContextForInput()
	if err != nil {
		panic(err)
	}

	// TODO: support macOS and window
	pAVInputFormat := avformat.FindInputByShortName("x11grab")
	if pAVInputFormat == nil {
		panic(errors.New("pAVInputFormat is nil"))
	}

	optionsScreen := avutil.NewDictionary()
	err = optionsScreen.Set("preset", "ultrafast")
	if err != nil {
		panic(err)
	}
	err = optionsScreen.Set("draw_mouse", "0")
	if err != nil {
		panic(err)
	}
	err = optionsScreen.Set("video_size", fmt.Sprintf("%dx%d", width, height))
	if err != nil {
		panic(err)
	}
	err = optionsScreen.Set("r", "10")
	if err != nil {
		panic(err)
	}

	err = pAVFormatContext.OpenInput(":0.0+0,0", pAVInputFormat, optionsScreen)
	if err != nil {
		panic(err)
	}

	err = pAVFormatContext.FindStreamInfo([]*avutil.Dictionary{optionsScreen})
	if err != nil {
		panic(err)
	}

	var pAVCodecContext *avcodec.Context

	for _, s := range pAVFormatContext.Streams() {
		pAVCodecContext = s.CodecContext()
		if pAVCodecContext.CodecType() == avutil.MediaTypeVideo {
			break
		}
	}

	pAVCodec := avcodec.FindDecoderByID(pAVCodecContext.CodecID())
	if pAVCodec == nil {
		panic(errors.New("pAVCodec is nil"))
	}

	err = pAVCodecContext.OpenWithCodec(pAVCodec, nil)
	if err != nil {
		panic(err)
	}

	decPacket, err := avcodec.NewPacket()
	if err != nil {
		panic(err)
	}

	defer encFrame.Free()
	defer encPacket.Free()
	defer swsContext.Free()
	defer codecContext.Free()
	defer pAVFormatContext.Free()
	defer pAVFormatContext.CloseInput()
	defer options.Free()
	defer optionsScreen.Free()
	defer close(dataCh)

	index := 0
	for {
		select {
		case <-streamContext.Done():
			return
		default:
			//now := time.Now()
			frameDone, err := pAVFormatContext.ReadFrame(decPacket)
			if err != nil {
				logger.Error(err)
				return
			}

			if !frameDone {
				return
			}

			_, err = pAVCodecContext.DecodeVideo(decPacket, func(decFrame *avutil.Frame) {
				err = encFrame.MakeWritable()
				if err != nil {
					panic(err)
				}

				swsContext.Scale(decFrame, 0, height, encFrame)
				encFrame.SetPTS(int64(index))
				index++

				encPacket.SetData(nil)
				encPacket.SetSize(0)

				_, err = codecContext.EncodeVideo(encPacket, encFrame, func(data []byte) {
					cData := make([]byte, len(data))
					copy(cData, data)
					dataCh <- cData
				})
				if err != nil {
					panic(err)
				}
				decFrame.FreeData(0)
			})

			if err != nil {
				logger.Error(err)
				return
			}
			//logger.Debug(time.Now().Sub(now))
		}
	}
}

func StreamReceive(streamCtx context.Context, width, height int, senderCh chan []byte, onImage func(img *image.YCbCr)) {
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

	defer packet.Free()
	defer codecContext.Free()

	ySize := width * height
	cdSize := width * height / 4
	crSize := width * height / 4

	needParse := true
	for {
		receipt, ok := <-senderCh
		if !ok {
			return
		}

		data := receipt
		dataSize := len(data)

		for dataSize > 0 {
			if needParse {
				ret, err := parserContext.Parse(data, dataSize, packet)
				if err != nil {
					panic(err)
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

			onFrame := func(f *avutil.Frame) {
				yData := C.GoBytes(f.Data(0), C.int(ySize))
				cbData := C.GoBytes(f.Data(1), C.int(cdSize))
				crData := C.GoBytes(f.Data(2), C.int(crSize))
				img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
				for i := 0; i < ySize; i++ {
					img.Y[i] = yData[i]
					if i < cdSize {
						img.Cb[i] = cbData[i]
					}
					if i < crSize {
						img.Cr[i] = crData[i]
					}
				}

				onImage(img)
			}

			_, err = codecContext.DecodeVideo(packet, onFrame)

			if err != nil {
				panic(err)
			}
			if needParse {
				parserContext.Free()
				needParse = false
			} else {
				C.free(packet.Data())
			}
		}
	}
}
