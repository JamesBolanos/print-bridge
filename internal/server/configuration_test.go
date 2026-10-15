package server

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"print-bridge/internal/config"
)

func TestInvalidRoutingNeverFallsBackToPhysical(t *testing.T) {
	for _, name := range []string{"missing destination", "unknown version", "invalid preview", "invalid origin"} {
		t.Run(name, func(t *testing.T) {
			cfg := config.Default()
			switch name {
			case "missing destination":
				cfg.DestinationType = ""
			case "unknown version":
				cfg.ConfigVersion = 99
			case "invalid preview":
				cfg.Preview.LabelWidthInches = 0
			case "invalid origin":
				cfg.AllowedOrigins = []string{"not an origin"}
			}
			srv := New(cfg, nil)
			t.Cleanup(srv.CaptureStore().Close)
			require.Error(t, srv.Start())
			assert.False(t, srv.Status().Running)
			assert.NotEmpty(t, srv.Status().Error)
			assert.Nil(t, srv.listener)
			dials := 0
			router := NewRouter(Options{Config: cfg, DialContext: func(context.Context, string, string) (net.Conn, error) {
				dials++
				return nil, assert.AnError
			}})
			w := submitPrint(router, []byte(`{"printerHostname":"printer.local","text":"DATA"}`))
			assert.Equal(t, http.StatusServiceUnavailable, w.Code)
			w = httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/status?host=printer.local", nil))
			assert.Equal(t, http.StatusServiceUnavailable, w.Code)
			assert.Zero(t, dials)
		})
	}
}

func TestMalformedSavedVirtualConfigCannotStartListener(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	original := `{"configVersion":1,"destinationType":"virtual","preview":`
	require.NoError(t, os.WriteFile(path, []byte(original), 0o600))
	cfg, result, err := config.LoadOrCreate(path)
	require.NoError(t, err)
	require.True(t, result.RecoveryRequired)
	srv := New(cfg, nil)
	t.Cleanup(srv.CaptureStore().Close)
	require.Error(t, srv.Start())
	assert.False(t, srv.Status().Running)
	unchanged, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, original, string(unchanged))
}

func TestRestartWithVirtualConfigReplacesPhysicalListener(t *testing.T) {
	cfg := config.Default()
	cfg.HTTPPort = freePort(t)
	srv := New(cfg, nil)
	t.Cleanup(func() {
		assert.NoError(t, srv.Stop(context.Background()))
		srv.CaptureStore().Close()
	})
	require.NoError(t, srv.Start())
	oldListener := srv.listener
	cfg.DestinationType = config.DestinationVirtual
	require.NoError(t, srv.Restart(cfg))
	assert.True(t, srv.Status().Running)
	assert.NotSame(t, oldListener, srv.listener)
	_, err := oldListener.Accept()
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "closed"))
	assert.Equal(t, config.DestinationVirtual, srv.cfg.DestinationType)
}
