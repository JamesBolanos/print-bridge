// Package config loads, validates, and saves print-bridge settings.
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
	AppID = "com.jbolanosdiaz.printbridge"
	// AppName is the internal application name used in packaging and paths.
	AppName = "print-bridge"
	// AppDisplayName is the user-facing application name.
	AppDisplayName = "print-bridge"
	// AppDataDirName is the folder created under the user's config directory.
	AppDataDirName = "PrintBridge"

	// DefaultHTTPPort is the localhost port used by the bridge on first run.
	DefaultHTTPPort = 8080
	// DefaultPrinterPort is the common raw TCP printing port.
	DefaultPrinterPort = 9100
	// CurrentVersion identifies the persisted destination/preview schema.
	CurrentVersion = 1
	// DestinationPhysical selects raw TCP printing.
	DestinationPhysical = "physical"
	// DestinationVirtual selects local capture once virtual routing is available.
	DestinationVirtual = "virtual"
)

// DefaultAllowedOrigins lets new installations connect to the SSCC label maker.
// Loading an existing configuration preserves its saved allow-list.
var DefaultAllowedOrigins = []string{
	"https://sscc-labels.com",
	"https://www.sscc-labels.com",
}

// Config contains the persisted print bridge settings.
type Config struct {
	ConfigVersion         int           `json:"configVersion"`
	DestinationType       string        `json:"destinationType"`
	Preview               PreviewConfig `json:"preview"`
	HTTPPort              int           `json:"httpPort"`
	DefaultPrinterPort    int           `json:"defaultPrinterPort"`
	DefaultPrinterAddress string        `json:"defaultPrinterAddress"`
	AllowedOrigins        []string      `json:"allowedOrigins"`
}

// PreviewConfig holds rendering preferences without changing print payloads.
type PreviewConfig struct {
	LabelaryEnabled   bool    `json:"labelaryEnabled"`
	LabelWidthInches  float64 `json:"labelWidthInches"`
	LabelHeightInches float64 `json:"labelHeightInches"`
	PrintDensityDpmm  int     `json:"printDensityDpmm"`
}

// DefaultPreview returns rendering-off preferences for a 4-by-6-inch label.
func DefaultPreview() PreviewConfig {
	return PreviewConfig{LabelWidthInches: 4, LabelHeightInches: 6, PrintDensityDpmm: 8}
}

// LoadResult distinguishes first-run creation, in-memory migration, and repair.
// RecoveryRequired means the returned display values must not start a listener.
type LoadResult struct {
	Created          bool
	UsedDefaults     bool
	Migrated         bool
	RecoveryRequired bool
	Warning          string
}

// Default returns a fresh copy of the default configuration.
func Default() Config {
	origins := make([]string, len(DefaultAllowedOrigins))
	copy(origins, DefaultAllowedOrigins)

	return Config{
		ConfigVersion:         CurrentVersion,
		DestinationType:       DestinationPhysical,
		Preview:               DefaultPreview(),
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
	return filepath.Join(dir, "print-bridge.log"), nil
}

// LoadOrCreate reads config from path or creates a default config when missing.
//
// Valid legacy files migrate only in memory. Invalid files are left untouched;
// the returned recovery config has no destination and cannot start a listener.
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

	cfg, migrated, err := decodeConfig(data)
	if err != nil {
		recovery := Default()
		recovery.DestinationType = ""
		return recovery, LoadResult{
			UsedDefaults: true, RecoveryRequired: true,
			Warning: fmt.Sprintf("Configuration could not be loaded: %v. Listener remains stopped; the original file is unchanged. Repair %s and restart PrintBridge.", err, path),
		}, nil
	}

	return cfg, LoadResult{Migrated: migrated}, nil
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
	if cfg.ConfigVersion != CurrentVersion {
		return Config{}, fmt.Errorf("unsupported configVersion %d", cfg.ConfigVersion)
	}
	if cfg.DestinationType != DestinationPhysical && cfg.DestinationType != DestinationVirtual {
		return Config{}, fmt.Errorf("destinationType must be physical or virtual")
	}
	if !(cfg.Preview.LabelWidthInches > 0 && cfg.Preview.LabelWidthInches <= 15) {
		return Config{}, fmt.Errorf("preview.labelWidthInches must be finite and greater than 0, up to 15")
	}
	if !(cfg.Preview.LabelHeightInches > 0 && cfg.Preview.LabelHeightInches <= 15) {
		return Config{}, fmt.Errorf("preview.labelHeightInches must be finite and greater than 0, up to 15")
	}
	switch cfg.Preview.PrintDensityDpmm {
	case 6, 8, 12, 24:
	default:
		return Config{}, fmt.Errorf("preview.printDensityDpmm must be 6, 8, 12, or 24")
	}
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
