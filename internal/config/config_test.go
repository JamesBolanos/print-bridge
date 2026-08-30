package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultAllowedOriginsAreEmpty(t *testing.T) {
	cfg := Default()

	assert.Empty(t, cfg.AllowedOrigins)
}

func TestValidateOrigin(t *testing.T) {
	assert.NoError(t, ValidateOrigin("http://localhost:3000"))
	assert.NoError(t, ValidateOrigin("https://app.example.com"))
	assert.Error(t, ValidateOrigin("https://app.example.com/path"))
	assert.Error(t, ValidateOrigin("ftp://app.example.com"))
	assert.Error(t, ValidateOrigin("localhost:3000"))
}

func TestLoadOrCreateCreatesDefaultConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	cfg, result, err := LoadOrCreate(path)

	require.NoError(t, err)
	assert.True(t, result.Created)
	assert.True(t, result.UsedDefaults)
	assert.Equal(t, Default(), cfg)

	_, err = os.Stat(path)
	assert.NoError(t, err)
}

func TestSaveRejectsInvalidConfig(t *testing.T) {
	cfg := Default()
	cfg.HTTPPort = 80

	err := Save(filepath.Join(t.TempDir(), "config.json"), cfg)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "httpPort")
}

func TestLoadOrCreateRestoresDefaultsForMalformedConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	require.NoError(t, os.WriteFile(path, []byte("{bad json"), 0o644))

	cfg, result, err := LoadOrCreate(path)

	require.NoError(t, err)
	assert.True(t, result.UsedDefaults)
	assert.NotEmpty(t, result.Warning)
	assert.Equal(t, Default(), cfg)
}
