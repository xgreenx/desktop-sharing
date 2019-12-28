package sharingnode

// #include <stdlib.h>
import "C"
import (
	"context"
	"github.com/imkira/go-libav/avcodec"
	"github.com/imkira/go-libav/avutil"
	"github.com/pkg/errors"
	"image"
)

func StreamSend(streamContext context.Context, width, height int, screenShotChan chan *screenShotTime, onData func([]byte)) {
	//avutil.SetLogLevel(avutil.LogLevelTrace)

	codec := avcodec.FindEncoderByName("libx264")
	if codec == nil {
		panic(errors.New("Codec not found"))
	}
	codecContext, err := avcodec.NewContextWithCodec(codec)
	if err != nil {
		panic(err)
	}
	//context.SetCodecType(avutil.MediaTypeVideo)

	frame, err := avutil.NewFrame()
	if err != nil {
		panic(err)
	}

	packet, err := avcodec.NewPacket()
	if err != nil {
		panic(err)
	}

	//context.SetBitRate(90000)
	codecContext.SetWidth(width)
	codecContext.SetHeight(height)
	codecContext.SetTimeBase(avutil.NewRational(1, 10))
	codecContext.SetFrameRate(avutil.NewRational(10, 1))
	//context.SetGOPSize(12)
	codecContext.SetMaxBFrames(0)
	codecContext.SetPixelFormat(avutil.PIX_FMT_YUV444P)

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

	frame.SetWidth(codecContext.Width())
	frame.SetHeight(codecContext.Height())
	frame.SetPixelFormat(codecContext.PixelFormat())

	err = frame.GetBuffer()
	if err != nil {
		panic(err)
	}

	defer frame.Free()
	defer packet.Free()
	defer codecContext.Free()

	index := 0
	for {
		sc, ok := <-screenShotChan
		if !ok {
			return
		}

		img := sc.image
		err = frame.MakeWritable()
		if err != nil {
			panic(err)
		}

		frame.SetData(0, img.Y)
		frame.SetData(1, img.Cb)
		frame.SetData(2, img.Cr)
		frame.SetPTS(int64(index))
		index++

		_, err = codecContext.EncodeVideo(packet, frame, func(data []byte) {
			cData := make([]byte, len(data))
			copy(cData, data)
			onData(cData)
		})
		if err != nil {
			panic(err)
		}
		frame.FreeData(0)
		frame.FreeData(1)
		frame.FreeData(2)
	}
}

func StreamReceive(streamCtx context.Context, width, height int, senderCh chan *dataTime, onImage func(img *image.YCbCr)) {
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
	cdSize := width * height
	crSize := width * height

	needParse := true
	for {
		receipt, ok := <-senderCh
		if !ok {
			return
		}

		data := receipt.data
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
				img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio444)
				for i := 0; i < ySize; i++ {
					img.Y[i] = yData[i]
					img.Cb[i] = cbData[i]
					img.Cr[i] = crData[i]
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
