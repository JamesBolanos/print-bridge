package config

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const legacyConfig = `{"httpPort":8181,"defaultPrinterPort":9200,"defaultPrinterAddress":"printer.local","allowedOrigins":["https://app.example.com"]}`
const modernConfig = `{"configVersion":1,"destinationType":"virtual","httpPort":8181,"defaultPrinterPort":9200,"defaultPrinterAddress":"printer.local","allowedOrigins":[],"preview":{"labelaryEnabled":true,"labelWidthInches":2.5,"labelHeightInches":3,"printDensityDpmm":12}}`

func TestLegacyMigrationPreservesSettingsUntilExplicitSave(t *testing.T) {
	for _, data := range []string{legacyConfig, strings.Replace(legacyConfig, "{", `{"configVersion":0,`, 1)} {
		path := filepath.Join(t.TempDir(), "config.json")
		require.NoError(t, os.WriteFile(path, []byte(data), 0o600))
		cfg, result, err := LoadOrCreate(path)
		require.NoError(t, err)
		assert.Equal(t, LoadResult{Migrated: true}, result)
		assert.Equal(t, CurrentVersion, cfg.ConfigVersion)
		assert.Equal(t, DestinationPhysical, cfg.DestinationType)
		assert.Equal(t, DefaultPreview(), cfg.Preview)
		assert.Equal(t, 8181, cfg.HTTPPort)
		assert.Equal(t, 9200, cfg.DefaultPrinterPort)
		assert.Equal(t, "printer.local", cfg.DefaultPrinterAddress)
		assert.Equal(t, []string{"https://app.example.com"}, cfg.AllowedOrigins)
		unchanged, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, data, string(unchanged))
		require.NoError(t, Save(path, cfg))
		reloaded, result, err := LoadOrCreate(path)
		require.NoError(t, err)
		assert.Equal(t, LoadResult{}, result)
		assert.Equal(t, cfg, reloaded)
		saved, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Contains(t, string(saved), `"configVersion": 1`)
	}
}

func TestVersionedConfigRoundTripAndDestinationSwitch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	require.NoError(t, os.WriteFile(path, []byte(modernConfig), 0o600))
	cfg, result, err := LoadOrCreate(path)
	require.NoError(t, err)
	assert.Equal(t, LoadResult{}, result)
	assert.Equal(t, DestinationVirtual, cfg.DestinationType)
	assert.Equal(t, PreviewConfig{LabelaryEnabled: true, LabelWidthInches: 2.5, LabelHeightInches: 3, PrintDensityDpmm: 12}, cfg.Preview)
	for _, destination := range []string{DestinationPhysical, DestinationVirtual} {
		cfg.DestinationType = destination
		require.NoError(t, Save(path, cfg))
		reloaded, result, err := LoadOrCreate(path)
		require.NoError(t, err)
		assert.Equal(t, LoadResult{}, result)
		assert.Equal(t, cfg, reloaded)
		assert.Equal(t, "printer.local", reloaded.DefaultPrinterAddress)
		assert.Equal(t, 9200, reloaded.DefaultPrinterPort)
	}
	files, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	require.Len(t, files, 1, "atomic saves must clean up temporary files")
	assert.Equal(t, "config.json", files[0].Name())
}

func TestVersionedPreviewDefaultsOnlyFillOmittedValues(t *testing.T) {
	for _, preview := range []string{"", `,"preview":{}`, `,"preview":{"labelHeightInches":2}`} {
		data := `{"configVersion":1,"destinationType":"virtual","httpPort":8080,"defaultPrinterPort":9100` + preview + `}`
		cfg, migrated, err := decodeConfig([]byte(data))
		require.NoError(t, err)
		assert.False(t, migrated)
		assert.Equal(t, DestinationVirtual, cfg.DestinationType)
		assert.False(t, cfg.Preview.LabelaryEnabled)
		assert.Equal(t, float64(4), cfg.Preview.LabelWidthInches)
		assert.Equal(t, 8, cfg.Preview.PrintDensityDpmm)
		if strings.Contains(preview, "labelHeightInches") {
			assert.Equal(t, float64(2), cfg.Preview.LabelHeightInches)
		}
	}
}

func TestInvalidFilesRequireRepairWithoutChangingOriginal(t *testing.T) {
	cases := map[string]string{
		"malformed": `{bad`, "null": `null`, "array": `[]`, "empty object": `{}`,
		"future version":          strings.Replace(modernConfig, `"configVersion":1`, `"configVersion":2`, 1),
		"null version":            strings.Replace(modernConfig, `"configVersion":1`, `"configVersion":null`, 1),
		"string version":          strings.Replace(modernConfig, `"configVersion":1`, `"configVersion":"1"`, 1),
		"missing destination":     strings.Replace(modernConfig, `"destinationType":"virtual",`, "", 1),
		"null destination":        strings.Replace(modernConfig, `"virtual"`, `null`, 1),
		"invalid destination":     strings.Replace(modernConfig, `"virtual"`, `"virtul"`, 1),
		"ambiguous legacy":        strings.Replace(modernConfig, `"configVersion":1,`, "", 1),
		"mixed case legacy":       strings.Replace(strings.Replace(modernConfig, `"configVersion":1,`, "", 1), "destinationType", "DestinationType", 1),
		"duplicate destination":   strings.Replace(modernConfig, `"destinationType":"virtual"`, `"destinationType":"virtual","destinationType":"physical"`, 1),
		"unknown field":           strings.Replace(modernConfig, "labelaryEnabled", "labeleryEnabled", 1),
		"duplicate preview field": strings.Replace(modernConfig, `"labelaryEnabled":true`, `"labelaryEnabled":true,"labelaryEnabled":false`, 1),
		"null preview":            strings.Replace(modernConfig, `{"labelaryEnabled":true,"labelWidthInches":2.5,"labelHeightInches":3,"printDensityDpmm":12}`, `null`, 1),
		"null width":              strings.Replace(modernConfig, `"labelWidthInches":2.5`, `"labelWidthInches":null`, 1),
		"zero width":              strings.Replace(modernConfig, `"labelWidthInches":2.5`, `"labelWidthInches":0`, 1),
		"null opt in":             strings.Replace(modernConfig, `"labelaryEnabled":true`, `"labelaryEnabled":null`, 1),
		"string opt in":           strings.Replace(modernConfig, `"labelaryEnabled":true`, `"labelaryEnabled":"true"`, 1),
		"bad origin":              strings.Replace(modernConfig, `"allowedOrigins":[]`, `"allowedOrigins":["not an origin"]`, 1),
		"trailing object":         modernConfig + `{}`,
	}
	for name, original := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			require.NoError(t, os.WriteFile(path, []byte(original), 0o600))
			cfg, result, err := LoadOrCreate(path)
			require.NoError(t, err, "repairable content errors should still allow the desktop to open")
			assert.True(t, result.RecoveryRequired)
			assert.False(t, result.Migrated)
			assert.Contains(t, result.Warning, path)
			assert.Empty(t, cfg.DestinationType)
			assert.False(t, cfg.Preview.LabelaryEnabled)
			_, err = Normalize(cfg)
			assert.Error(t, err)
			assert.Error(t, Save(path, cfg), "recovery values cannot overwrite the file")
			unchanged, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, original, string(unchanged))
		})
	}
}

func TestPreviewValidationAndFailedSavePreserveExistingConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	valid := Default()
	valid.DestinationType = DestinationVirtual
	require.NoError(t, Save(path, valid))
	original, err := os.ReadFile(path)
	require.NoError(t, err)
	for _, value := range []float64{0, -1, 15.01, math.NaN(), math.Inf(1), math.Inf(-1)} {
		for _, width := range []bool{true, false} {
			cfg := valid
			if width {
				cfg.Preview.LabelWidthInches = value
			} else {
				cfg.Preview.LabelHeightInches = value
			}
			assert.Error(t, Save(path, cfg))
		}
	}
	for _, density := range []int{0, -1, 7, 203} {
		cfg := valid
		cfg.Preview.PrintDensityDpmm = density
		assert.Error(t, Save(path, cfg))
	}
	for _, density := range []int{6, 8, 12, 24} {
		cfg := valid
		cfg.Preview.PrintDensityDpmm = density
		cfg.Preview.LabelWidthInches, cfg.Preview.LabelHeightInches = 0.01, 15
		_, err := Normalize(cfg)
		assert.NoError(t, err)
	}
	unchanged, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, original, unchanged)
	var onDisk Config
	require.NoError(t, json.Unmarshal(unchanged, &onDisk))
	assert.Equal(t, DestinationVirtual, onDisk.DestinationType)
}

func TestReadErrorsAreNotTreatedAsFirstRun(t *testing.T) {
	_, result, err := LoadOrCreate(t.TempDir()) // a directory cannot be a config file
	assert.Error(t, err)
	assert.False(t, result.Created)
	assert.False(t, result.UsedDefaults)
}
