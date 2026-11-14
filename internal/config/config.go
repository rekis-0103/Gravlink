package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Config holds persisted and runtime configuration for Gravlink.
type Config struct {
	Port            int    `json:"port"`
	Workspace       string `json:"workspace"`
	AntigravityDir  string `json:"antigravity_dir"`
	DataDir         string `json:"data_dir"`
	PasswordHash    string `json:"password_hash"`
	SessionSecret   string `json:"session_secret"`
	UseTLS          bool   `json:"use_tls"`
	StrictSubnet    bool   `json:"strict_subnet"`
	SessionTTLHours int    `json:"session_ttl_hours"`
}

// DefaultDataDir returns ~/.gravlink.
func DefaultDataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".gravlink"), nil
}

// DefaultAntigravityDir returns ~/.gemini/antigravity.
func DefaultAntigravityDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".gemini", "antigravity"), nil
}

// LoadOrInit loads config.json from dataDir or creates a default config with a fresh session secret.
func LoadOrInit(dataDir string) (*Config, error) {
	if dataDir == "" {
		var err error
		dataDir, err = DefaultDataDir()
		if err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}

	cfgPath := filepath.Join(dataDir, "config.json")
	cfg := &Config{
		Port:            8443,
		DataDir:         dataDir,
		UseTLS:          true,
		StrictSubnet:    true,
		SessionTTLHours: 24,
	}

	if raw, err := os.ReadFile(cfgPath); err == nil {
		if err := json.Unmarshal(raw, cfg); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	if cfg.DataDir == "" {
		cfg.DataDir = dataDir
	}
	if cfg.AntigravityDir == "" {
		agDir, err := DefaultAntigravityDir()
		if err == nil {
			cfg.AntigravityDir = agDir
		}
	}
	if cfg.Port <= 0 {
		cfg.Port = 8443
	}
	if cfg.SessionTTLHours <= 0 {
		cfg.SessionTTLHours = 24
	}
	if cfg.SessionSecret == "" {
		secret := make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return nil, err
		}
		cfg.SessionSecret = hex.EncodeToString(secret)
		if err := Save(cfg); err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

// Save persists configuration to config.json with owner-only permissions.
func Save(cfg *Config) error {
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	cfgPath := filepath.Join(cfg.DataDir, "config.json")
	return os.WriteFile(cfgPath, data, 0o600)
}
