package config

import (
	"fmt"
	"github.com/spf13/viper"
	"io/ioutil"
	"os"
)

type Config struct {
	Path  string
	Name  string
	Type  string
	Viper *viper.Viper
}

func NewConfig(path string, name string, t string) *Config {
	v := viper.New()
	config := &Config{
		Viper: v,
		Path:  path,
		Name:  name,
		Type:  t,
	}

	v.SetConfigName(config.Name)
	v.SetConfigType(config.Type)
	return config
}

func (b *Config) LoadConfig() error {
	b.Viper.AddConfigPath(b.Path)
	if err := b.Viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			err = b.WriteConfig()
			if err != nil {
				return err
			}

			return b.Viper.ReadInConfig()
		} else {
			return err
		}
	}
	return nil
}

func (b *Config) WriteConfig() error {
	b.Viper.AddConfigPath(b.Path)
	if err := b.Viper.WriteConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			err := os.MkdirAll(b.Path, 0777)
			if err != nil {
				return err
			}

			err = ioutil.WriteFile(fmt.Sprintf("%s/%s.%s", b.Path, b.Name, b.Type), []byte{}, 0777)
			if err != nil {
				return err
			}

			err = b.Viper.WriteConfig()
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}

	return nil
}
