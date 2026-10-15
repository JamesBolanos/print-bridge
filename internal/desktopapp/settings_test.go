package desktopapp

import (
	"errors"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"print-bridge/internal/config"
)

func TestSettingsApplyDestinationAndPreview(t *testing.T) {
	base := config.Default()
	base.DestinationType = config.DestinationVirtual
	base.DefaultPrinterAddress = "saved-printer.local"
	base.DefaultPrinterPort = 9200
	base.Preview = config.PreviewConfig{LabelaryEnabled: true, LabelWidthInches: 2, LabelHeightInches: 3, PrintDensityDpmm: 12}
	base.AllowedOrigins = []string{"https://old.example.com"}
	got, err := settingsConfig(
		base, " 8181 ", "9200", " printer.local ", "https://new.example.com\nhttp://localhost:3000",
		"Physical", false, "4", "6", "8",
	)
	require.NoError(t, err)
	assert.Equal(t, config.CurrentVersion, got.ConfigVersion)
	assert.Equal(t, config.DestinationPhysical, got.DestinationType)
	assert.Equal(t, config.DefaultPreview(), got.Preview)
	assert.Equal(t, 8181, got.HTTPPort)
	assert.Equal(t, 9200, got.DefaultPrinterPort)
	assert.Equal(t, "printer.local", got.DefaultPrinterAddress)
	assert.Equal(t, []string{"https://new.example.com", "http://localhost:3000"}, got.AllowedOrigins)

	got, err = settingsConfig(
		base, "8181", "9200", "saved-printer.local", "https://old.example.com",
		"Virtual (capture only)", true, "2.5", "3", "12",
	)
	require.NoError(t, err)
	assert.Equal(t, config.DestinationVirtual, got.DestinationType)
	assert.Equal(t, config.PreviewConfig{LabelaryEnabled: true, LabelWidthInches: 2.5, LabelHeightInches: 3, PrintDensityDpmm: 12}, got.Preview)
	assert.Equal(t, "saved-printer.local", got.DefaultPrinterAddress, "mode changes preserve physical settings")
	assert.Equal(t, 9200, got.DefaultPrinterPort)
	assert.Equal(t, config.DestinationVirtual, base.DestinationType, "editing must not mutate active config before Save")
	assert.Equal(t, 8080, base.HTTPPort)
}

func TestSettingsCannotMakeRecoveryConfigRunnable(t *testing.T) {
	base := config.Default()
	base.DestinationType = "" // display-only recovery state from LoadOrCreate
	_, err := settingsConfig(base, "8080", "9100", "", "", "Physical", false, "4", "6", "8")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "destinationType")
}

func TestSettingsRejectInvalidEdits(t *testing.T) {
	for _, edits := range [][9]string{
		{"bad", "9100", "", "", "Physical", "false", "4", "6", "8"},
		{"8080", "bad", "", "", "Physical", "false", "4", "6", "8"},
		{"80", "9100", "", "", "Physical", "false", "4", "6", "8"},
		{"8080", "9100", "", "invalid-origin", "Physical", "false", "4", "6", "8"},
		{"8080", "9100", "", "", "invalid", "false", "4", "6", "8"},
		{"8080", "9100", "", "", "Virtual (capture only)", "true", "NaN", "6", "8"},
		{"8080", "9100", "", "", "Physical", "false", "4", "6", "7"},
	} {
		_, err := settingsConfig(config.Default(), edits[0], edits[1], edits[2], edits[3], edits[4], edits[5] == "true", edits[6], edits[7], edits[8])
		assert.Error(t, err)
	}
}

func TestInspectorURLUsesLoopbackAndCurrentPort(t *testing.T) {
	target, err := inspectorURL(8234)
	require.NoError(t, err)
	assert.Equal(t, "http://127.0.0.1:8234/inspector/", target.String())
}

func TestLaunchInspectorOpensExpectedURLAndReportsBrowserFailure(t *testing.T) {
	var got *url.URL
	require.NoError(t, launchInspector(8088, func(target *url.URL) error {
		got = target
		return nil
	}))
	require.NotNil(t, got)
	assert.Equal(t, "http://127.0.0.1:8088/inspector/", got.String())

	openErr := errors.New("browser unavailable")
	err := launchInspector(8088, func(*url.URL) error { return openErr })
	require.ErrorIs(t, err, openErr)
	assert.ErrorContains(t, launchInspector(8088, nil), "system browser is unavailable")
	assert.Error(t, launchInspector(80, func(*url.URL) error { return nil }))
}

func TestModeLabel(t *testing.T) {
	assert.Equal(t, "Physical", destinationLabel(config.DestinationPhysical))
	assert.Equal(t, "Virtual (capture only)", destinationLabel(config.DestinationVirtual))
	assert.Equal(t, "Unavailable (repair required)", destinationLabel(""))
}
