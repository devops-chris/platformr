package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// BrandingConfig holds optional display overrides set by the connected org.
// Cached in local config so every command reflects the org's branding.
type BrandingConfig struct {
	Name        string `toml:"name"`
	Description string `toml:"description"`
}

// LocalConfig is the minimal config stored at ~/.config/platformr/config.toml.
// It is written by `platformr connect` and read on every command.
type LocalConfig struct {
	ConnectedOrg string         `toml:"connected_org"`
	Branding     BrandingConfig `toml:"branding"`
}

func LocalConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "platformr", "config.toml"), nil
}

func LoadLocal() (*LocalConfig, error) {
	path, err := LocalConfigPath()
	if err != nil {
		return nil, err
	}
	var cfg LocalConfig
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return &cfg, nil
}

func SaveLocal(cfg *LocalConfig) error {
	path, err := LocalConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(cfg)
}
