package config

const StreamID = "/stream/1.0.0"
const EventID = "/event/1.0.0"

type SharingOptions struct {
	StreamOptions         map[string]string
	ScreenGrabbingOptions map[string]string
}

type SharingConfig struct {
	*BootstrapConfig
	SharingOptions *SharingOptions
}

// Contains tells whether a contains x.
func contains(a []string, x string) bool {
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

	for _, p := range []string{StreamID, EventID} {
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

func (b *SharingConfig) UpdateDefaults() {
	b.BootstrapConfig.UpdateDefaults()

	v := b.Viper
	v.SetDefault("sharing.stream", &b.SharingOptions.StreamOptions)
	v.SetDefault("sharing.screengrabbing", &b.SharingOptions.ScreenGrabbingOptions)
}
