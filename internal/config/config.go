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
	AppID          = "com.jbolanosdiaz.printerbridge"
	AppName        = "printer-bridge"
	AppDisplayName = "printer-bridge"
	AppDataDirName = "PrinterBridge"

	DefaultHTTPPort    = 8080
	DefaultPrinterPort = 9100
)

var DefaultAllowedOrigins = []string{}

type Config struct {
	HTTPPort              int      `json:"httpPort"`
	DefaultPrinterPort    int      `json:"defaultPrinterPort"`
	DefaultPrinterAddress string   `json:"defaultPrinterAddress"`
	AllowedOrigins        []string `json:"allowedOrigins"`
}

type LoadResult struct {
	Created      bool
	UsedDefaults bool
	Warning      string
}

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

func AppDataDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory: %w", err)
	}
	return filepath.Join(configDir, AppDataDirName), nil
}

func ConfigPath() (string, error) {
	dir, err := AppDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func LogPath() (string, error) {
	dir, err := AppDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "printer-bridge.log"), nil
}

func LoadOrCreate(path string) (Config, LoadResult, error) {
	if path == "" {
		var err error
		path, err = ConfigPath()
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

func Save(path string, cfg Config) error {
	normalized, err := Normalize(cfg)
	if err != nil {
		return err
	}

	if path == "" {
		path, err = ConfigPath()
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
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
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
