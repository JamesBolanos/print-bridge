package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// decodeConfig applies defaults only to omitted optional fields, distinguishes
// legacy files from versioned files, and validates before returning a runnable
// configuration. A legacy file containing new fields is ambiguous, not a reason
// to replace an explicitly saved destination with Physical.
func decodeConfig(data []byte) (Config, bool, error) {
	fields, err := configObject(data, "configuration", "configVersion", "destinationType", "preview", "httpPort", "defaultPrinterPort", "defaultPrinterAddress", "allowedOrigins")
	if err != nil {
		return Config{}, false, err
	}
	version := 0
	if raw, present := fields["configVersion"]; present {
		if isNull(raw) {
			return Config{}, false, fmt.Errorf("configVersion must not be null")
		}
		if err := json.Unmarshal(raw, &version); err != nil {
			return Config{}, false, fmt.Errorf("invalid configVersion: %w", err)
		}
	}
	if version != 0 && version != CurrentVersion {
		return Config{}, false, fmt.Errorf("unsupported configVersion %d", version)
	}
	if version == 0 {
		for _, name := range []string{"destinationType", "preview"} {
			if _, present := fields[name]; present {
				return Config{}, false, fmt.Errorf("%s requires configVersion %d", name, CurrentVersion)
			}
		}
	}
	if raw, present := fields["preview"]; present {
		preview, err := configObject(raw, "preview", "labelaryEnabled", "labelWidthInches", "labelHeightInches", "printDensityDpmm")
		if err != nil {
			return Config{}, false, err
		}
		for name, value := range preview {
			if isNull(value) {
				return Config{}, false, fmt.Errorf("preview.%s must not be null", name)
			}
		}
	}
	// Existing port fields remain required, while omitted preview preferences
	// get safe defaults. An explicit zero is left intact for validation to reject.
	cfg := Config{Preview: DefaultPreview()}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, false, err
	}
	if version == 0 {
		cfg.ConfigVersion = CurrentVersion
		cfg.DestinationType = DestinationPhysical
	}
	normalized, err := Normalize(cfg)
	return normalized, version == 0, err
}

func isNull(raw json.RawMessage) bool { return bytes.Equal(bytes.TrimSpace(raw), []byte("null")) }

// configObject rejects unknown, mis-cased, and duplicate keys before struct
// decoding can ignore or merge them. That avoids ambiguous saved destinations
// and preview opt-in values while retaining precise repair errors.
func configObject(data []byte, name string, allowed ...string) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, fmt.Errorf("%s must be a JSON object", name)
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("invalid %s: %w", name, err)
		}
		key, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("invalid field in %s", name)
		}
		known := false
		for _, field := range allowed {
			if key == field {
				known = true
				break
			}
		}
		if !known {
			return nil, fmt.Errorf("unknown %s field %q", name, key)
		}
		if _, exists := fields[key]; exists {
			return nil, fmt.Errorf("duplicate %s field %q", name, key)
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, fmt.Errorf("invalid %s.%s: %w", name, key, err)
		}
		fields[key] = raw
	}
	if _, err := decoder.Token(); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", name, err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("unexpected data after %s", name)
	}
	return fields, nil
}
