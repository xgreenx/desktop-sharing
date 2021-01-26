module github.com/xgreenx/desktop-sharing

go 1.13

replace (
	fyne.io/fyne => github.com/xgreenx/fyne v1.3.3
	github.com/imkira/go-libav => github.com/xgreenx/go-libav v0.0.0-20200201202111-d53b350debca
)

require (
	fyne.io/fyne v1.1.2
	github.com/BurntSushi/xgb v0.0.0-20200324125942-20f126ea2843 // indirect
	github.com/gen2brain/shm v0.0.0-20200228170931-49f9650110c5 // indirect
	github.com/go-gl/glfw/v3.3/glfw v0.0.0-20200625191551-73d3c3675aa3
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
	github.com/pkg/errors v0.9.1
	github.com/spf13/pflag v1.0.5
	github.com/spf13/viper v1.6.1
	github.com/whyrusleeping/go-logging v0.0.1
)
