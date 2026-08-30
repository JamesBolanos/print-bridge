// Package config loads, validates, and saves printer-bridge settings.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	// AppID is the stable desktop application identifier.
	AppID = "com.jbolanosdiaz.printerbridge"
	// AppName is the internal application name used in packaging and paths.
	AppName = "printer-bridge"
	// AppDisplayName is the user-facing application name.
	AppDisplayName = "printer-bridge"
	// AppDataDirName is the folder created under the user's config directory.
	AppDataDirName = "PrinterBridge"

	// DefaultHTTPPort is the localhost port used by the bridge on first run.
	DefaultHTTPPort = 8080
	// DefaultPrinterPort is the common raw TCP printing port.
	DefaultPrinterPort = 9100
)

// DefaultAllowedOrigins is intentionally empty so new installs deny browser CORS requests.
var DefaultAllowedOrigins = []string{}

// Config contains the persisted printer bridge settings.
type Config struct {
	HTTPPort              int      `json:"httpPort"`
	DefaultPrinterPort    int      `json:"defaultPrinterPort"`
	DefaultPrinterAddress string   `json:"defaultPrinterAddress"`
	AllowedOrigins        []string `json:"allowedOrigins"`
}

// LoadResult describes whether loading config required creating or restoring defaults.
type LoadResult struct {
	Created      bool
	UsedDefaults bool
	Warning      string
}

// Default returns a fresh copy of the default configuration.
func Default() Config {
	origins := make([]string, len(DefaultAllowedOrigins))
	copy(origins, DefaultAllowedOrigins)

	return Config{
		HTTPPort:              DefaultHTTPPort,
		DefaultPrinterPort:    DefaultPrinterPort,
		DefaultPrinterAddress: "",
		AllowedOrigins:        origins,
	}
}

// AppDataDir returns the platform-appropriate directory for app data.
func AppDataDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory: %w", err)
	}
	return filepath.Join(configDir, AppDataDirName), nil
}

// DefaultConfigPath returns the default config.json path.
func DefaultConfigPath() (string, error) {
	dir, err := AppDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// LogPath returns the default rotating log file path.
func LogPath() (string, error) {
	dir, err := AppDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "printer-bridge.log"), nil
}

// LoadOrCreate reads config from path or creates a default config when missing.
//
// Malformed or invalid config is replaced with defaults so the desktop app can
// still start and show the user a recoverable warning.
func LoadOrCreate(path string) (Config, LoadResult, error) {
	if path == "" {
		var err error
		path, err = DefaultConfigPath()
		if err != nil {
			return Config{}, LoadResult{}, err
		}
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		cfg := Default()
		if err := Save(path, cfg); err != nil {
			return Config{}, LoadResult{}, err
		}
		return cfg, LoadResult{Created: true, UsedDefaults: true}, nil
	}
	if err != nil {
		return Config{}, LoadResult{}, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		cfg := Default()
		if saveErr := Save(path, cfg); saveErr != nil {
			return Config{}, LoadResult{}, saveErr
		}
		return cfg, LoadResult{
			UsedDefaults: true,
			Warning:      fmt.Sprintf("config was malformed and defaults were restored: %v", err),
		}, nil
	}

	normalized, err := Normalize(cfg)
	if err != nil {
		cfg := Default()
		if saveErr := Save(path, cfg); saveErr != nil {
			return Config{}, LoadResult{}, saveErr
		}
		return cfg, LoadResult{
			UsedDefaults: true,
			Warning:      fmt.Sprintf("config was invalid and defaults were restored: %v", err),
		}, nil
	}

	return normalized, LoadResult{}, nil
}

// Save validates and atomically writes config to path.
func Save(path string, cfg Config) error {
	normalized, err := Normalize(cfg)
	if err != nil {
		return err
	}

	if path == "" {
		path, err = DefaultConfigPath()
		if err != nil {
			return err
		}
	}

	data, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp config: %w", err)
	}
	if err := replaceFile(tmpName, path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}

	return nil
}

// Normalize validates config values and applies whitespace cleanup.
func Normalize(cfg Config) (Config, error) {
	if cfg.HTTPPort < 1024 || cfg.HTTPPort > 65535 {
		return Config{}, fmt.Errorf("httpPort must be between 1024 and 65535")
	}
	if cfg.DefaultPrinterPort < 1 || cfg.DefaultPrinterPort > 65535 {
		return Config{}, fmt.Errorf("defaultPrinterPort must be between 1 and 65535")
	}

	cfg.DefaultPrinterAddress = strings.TrimSpace(cfg.DefaultPrinterAddress)

	origins := make([]string, 0, len(cfg.AllowedOrigins))
	for _, origin := range cfg.AllowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "" {
			continue
		}
		if err := ValidateOrigin(origin); err != nil {
			return Config{}, err
		}
		origins = append(origins, origin)
	}
	cfg.AllowedOrigins = origins

	return cfg, nil
}

// ValidateOrigin ensures a CORS origin is only scheme, host, and optional port.
func ValidateOrigin(origin string) error {
	parsed, err := url.Parse(origin)
	if err != nil {
		return fmt.Errorf("allowed origin %q is invalid: %w", origin, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("allowed origin %q must use http or https", origin)
	}
	if parsed.Host == "" {
		return fmt.Errorf("allowed origin %q must include a host", origin)
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return fmt.Errorf("allowed origin %q must not include a path", origin)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
		return fmt.Errorf("allowed origin %q must be only scheme, host, and optional port", origin)
	}

	return nil
}
