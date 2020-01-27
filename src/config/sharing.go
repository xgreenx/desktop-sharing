package config

import "github.com/libp2p/go-libp2p-core/protocol"

const StreamID = protocol.ID("/stream/1.0.0")
const EventID = protocol.ID("/event/1.0.0")

type SharingOptions struct {
	StreamOptions         map[string]string
	ScreenGrabbingOptions map[string]string
}

type SharingConfig struct {
	*BootstrapConfig
	SharingOptions *SharingOptions
}

// Contains tells whether a contains x.
func contains(a []protocol.ID, x protocol.ID) bool {
	for _, n := range a {
		if x == n {
			return true
		}
	}
	return false
}

func NewSharingConfig(bootstrap *BootstrapConfig) *SharingConfig {
	config := &SharingConfig{
		BootstrapConfig: bootstrap,
		SharingOptions:  &SharingOptions{},
	}

	for _, p := range []protocol.ID{StreamID, EventID} {
		if !contains(config.Protocols, p) {
			config.Protocols = append(config.Protocols, p)
		}
	}

	config.SharingOptions.StreamOptions = map[string]string{
		"preset":    "ultrafast",
		"crf":       "37",
		"ar":        "44100",
		"r":         "10",
		"ac":        "2",
		"tune":      "zerolatency",
		"probesize": "32",
		"maxrate":   "750k",
		"bufsize":   "3000k",
	}

	config.SharingOptions.ScreenGrabbingOptions = map[string]string{
		"preset":     "ultrafast",
		"draw_mouse": "0",
		"r":          "10",
	}

	config.UpdateDefaults()

	return config
}

func (b *SharingConfig) UpdateDefaults() {
	b.BootstrapConfig.UpdateDefaults()

	v := b.Viper
	v.SetDefault("sharing.stream", b.SharingOptions.StreamOptions)
	v.SetDefault("sharing.screengrabbing", b.SharingOptions.ScreenGrabbingOptions)
}

func (b *SharingConfig) LoadConfig() error {
	err := b.BootstrapConfig.LoadConfig()
	if err != nil {
		return err
	}

	err = b.Viper.UnmarshalKey("sharing.stream", &b.SharingOptions.StreamOptions)
	if err != nil {
		return err
	}

	err = b.Viper.UnmarshalKey("sharing.screengrabbing", &b.SharingOptions.ScreenGrabbingOptions)
	if err != nil {
		return err
	}

	return nil
}
