// Package config handles loading and persisting the CLI configuration,
// most importantly the user's Discord token, in the location where CLI
// tools conventionally store their data.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// appDir is the folder name used inside the user's config directory.
const appDir = "discord-message-deleter"

// Config is the persisted CLI configuration.
type Config struct {
	Token string `json:"token"`
}

// Dir returns the directory where the configuration is stored,
// e.g. ~/.config/discord-message-deleter on Linux or the platform
// equivalent on macOS/Windows.
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("could not determine user config directory: %w", err)
	}
	return filepath.Join(base, appDir), nil
}

// Path returns the full path to the config file.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Load reads the configuration from disk. If the file does not exist an
// empty Config is returned without error, so callers can rely on flags or
// the environment instead.
func Load() (*Config, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("could not read config file: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config file is corrupt (%s): %w", path, err)
	}
	return &cfg, nil
}

// Save writes the configuration to disk with restrictive permissions,
// creating the directory if necessary. The token is sensitive, so the
// file is written 0600 (owner read/write only).
func (c *Config) Save() error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("could not create config directory: %w", err)
	}

	path := filepath.Join(dir, "config.json")
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("could not encode config: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("could not write config file: %w", err)
	}
	return nil
}
