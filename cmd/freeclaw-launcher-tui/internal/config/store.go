package configstore

import (
	"errors"
	"os"
	"path/filepath"

	freeclawconfig "github.com/sipeed/freeclaw/pkg/config"
)

const (
	configDirName  = ".freeclaw"
	configFileName = "config.json"
)

func ConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, configFileName), nil
}

func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, configDirName), nil
}

func Load() (*freeclawconfig.Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}
	return freeclawconfig.LoadConfig(path)
}

func Save(cfg *freeclawconfig.Config) error {
	if cfg == nil {
		return errors.New("config is nil")
	}
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	return freeclawconfig.SaveConfig(path, cfg)
}
