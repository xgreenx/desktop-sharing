module github.com/xgreenx/desktop-sharing

go 1.13

replace (
	fyne.io/fyne => github.com/xgreenx/fyne v1.1.3-0.20191228134522-4969471d1723
	github.com/imkira/go-libav => github.com/xgreenx/go-libav v0.0.0-20200126120735-7fad8d896b03
)

require (
	fyne.io/fyne v1.1.2
	github.com/BurntSushi/xgb v0.0.0-20160522181843-27f122750802 // indirect
	github.com/gen2brain/shm v0.0.0-20191025110947-b09d223a76f1 // indirect
	github.com/go-gl/glfw v0.0.0-20181213070059-819e8ce5125f
	github.com/go-vgo/robotgo v0.0.0-20191201151851-6417b546fec7
	github.com/imkira/go-libav v0.0.0-20190125075901-6bf952df9de5
	github.com/ipfs/go-log v0.0.1
	github.com/kbinani/screenshot v0.0.0-20190719135742-f06580e30cdc
	github.com/libp2p/go-libp2p v0.5.1
	github.com/libp2p/go-libp2p-autonat-svc v0.1.0
	github.com/libp2p/go-libp2p-circuit v0.1.4
	github.com/libp2p/go-libp2p-core v0.3.0
	github.com/libp2p/go-libp2p-discovery v0.2.0
	github.com/libp2p/go-libp2p-kad-dht v0.5.0
	github.com/multiformats/go-multiaddr v0.2.0
	github.com/pkg/errors v0.8.1
	github.com/spf13/pflag v1.0.5
	github.com/spf13/viper v1.6.1
	github.com/whyrusleeping/go-logging v0.0.1
)
