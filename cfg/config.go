package cfg

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

type (
	Config struct {
		Registry struct {
			BaseUrl string `mapstructure:"base_url"`
		} `mapstructure:"registry"`

		Local struct {
			Dest string `mapstructure:"destination_folder"`
		} `mapstructure:"local"`
	}
)

func Load(registryDir string) (config Config, err error) {
	v := viper.New()

	path := filepath.Join(registryDir, "config.yaml")

	if _, err = os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		path = filepath.Join(registryDir, "config.yml")

		if _, err = os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			return Config{}, fmt.Errorf("neither config.yaml nor config.yml exists in the %s folder", registryDir)
		}
	}

	v.SetConfigFile(path)

	if err := v.ReadInConfig(); err != nil {
		return config, fmt.Errorf("failed to read config file at %s: %s", path, err.Error())
	}

	if err := v.Unmarshal(&config); err != nil {
		return config, fmt.Errorf("failed to parse config file at %s: %s", path, err.Error())
	}

	config.Registry.BaseUrl = strings.TrimSuffix(config.Registry.BaseUrl, "/")

	// needs more validation logic here

	return config, nil
}
