package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"print-bridge/internal/capture"
	"print-bridge/internal/config"
)

func TestVirtualPrintCapturesWithoutPhysicalFallback(t *testing.T) {
	for _, tc := range []struct {
		name, host string
		port       int
		target     string
	}{
		{"omitted target", "", 0, ":9200"},
		{"supplied target", " printer.local ", 9101, "printer.local:9101"},
		{"IPv6 target", "::1", 9100, "[::1]:9100"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := virtualConfig()
			cfg.DefaultPrinterPort = 9200
			store, router := virtualRouter(t, cfg)
			payload := " \r\n\t^XA^FDcafé 世界^FS^XZ\x00<script>raw</script>\n "
			body, err := json.Marshal(PrintJobRequest{PrinterHostname: tc.host, PrinterPort: tc.port, Text: payload})
			require.NoError(t, err)
			w := submitPrint(router, body)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			var response map[string]string
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
			require.NotEmpty(t, response["jobId"])
			assert.Equal(t, map[string]string{"message": "OK", "destination": "virtual", "jobId": response["jobId"]}, response)
			job, err := store.Get(response["jobId"])
			require.NoError(t, err)
			assert.Equal(t, []byte(payload), job.Payload)
			assert.Equal(t, tc.target, job.Target)
			assert.Equal(t, "captured", job.CaptureState)
			assert.Equal(t, "disabled", job.PreviewState)
			require.Len(t, store.List(), 1)
		})
	}
}

func TestVirtualRejectsMalformedRequests(t *testing.T) {
	store, router := virtualRouter(t, virtualConfig())
	for _, body := range []string{
		``, `{`, `null`, `[]`, `"text"`, `{}`, `{"text":""}`,
		`{"text":null}`, `{"text":123}`, `{"text":false}`, `{"text":{}}`,
		`{"text":"ok","printerHostname":123}`, `{"text":"ok","printerHostname":null}`,
		`{"text":"ok","printerPort":"9100"}`, `{"text":"ok","printerPort":null}`,
		`{"text":"ok","printerPort":1.5}`, `{"text":"ok","printerPort":-1}`,
		`{"text":"ok","printerPort":65536}`, `{"text":"ok"} {}`, `{"text":"ok"} trailing`,
	} {
		t.Run(body, func(t *testing.T) {
			w := submitPrint(router, []byte(body))
			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			assert.NotEmpty(t, responseMessage(t, w))
			assert.Empty(t, store.List())
		})
	}
	// Whitespace is data, and a request-level routing field cannot bypass Virtual.
	w := submitPrint(router, []byte(`{"text":" \n\t ","destination":"physical"}`))
	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, store.List(), 1)
}

func TestVirtualStatusReportsCaptureReadiness(t *testing.T) {
	cfg := virtualConfig()
	cfg.DefaultPrinterPort = 9200
	cfg.Preview.LabelaryEnabled = true // readiness cannot depend on a renderer
	store, router := virtualRouter(t, cfg)
	for _, closed := range []bool{false, true} {
		if closed {
			store.Close()
		}
		for _, tc := range []struct {
			query, host string
			port        int
		}{
			{"", "", 9200}, {"?port=9101", "", 9101},
			{"?host=printer.local", "printer.local", 9200},
			{"?host=%20printer.local%20&port=9101", "printer.local", 9101},
		} {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/status"+tc.query, nil))
			assert.Equal(t, http.StatusOK, w.Code)
			assert.JSONEq(t, fmt.Sprintf(`{"reachable":%t,"destination":"virtual","host":%q,"port":%d}`, !closed, tc.host, tc.port), w.Body.String())
		}
	}
	for _, port := range []string{"bad", "0", "-1", "65536", "1.5"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/status?port="+port, nil))
		assert.Equal(t, http.StatusBadRequest, w.Code)
	}
	w := submitPrint(router, []byte(`{"printerHostname":"printer.local","text":"DATA"}`))
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Empty(t, store.List())
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ping", nil))
	assert.JSONEq(t, `{"message":"pong"}`, w.Body.String())
}

func TestVirtualLimitsAndPreviewPressure(t *testing.T) {
	store, router := virtualRouter(t, virtualConfig())
	limits := capture.DefaultLimits()
	payload := strings.Repeat("\x00", limits.JobBytes)
	body, err := json.Marshal(PrintJobRequest{Text: payload})
	require.NoError(t, err)
	w := submitPrint(router, body)
	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, store.List(), 1)
	assert.Equal(t, limits.JobBytes, store.List()[0].ByteCount)

	for _, body := range []string{
		`{"text":"` + strings.Repeat("x", limits.JobBytes+1) + `"}`,
		`{"text":"ok","printerHostname":"` + strings.Repeat("x", limits.TargetBytes) + `"}`,
		`{"text":"ok"}` + strings.Repeat(" ", int(limits.RequestBytes)),
		`{"text":"` + strings.Repeat("x", int(limits.RequestBytes)) + `"}`,
	} {
		reader := &countingReader{Reader: strings.NewReader(body)}
		req := httptest.NewRequest(http.MethodPost, "/print", reader)
		req.ContentLength = -1
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
		assert.LessOrEqual(t, int64(reader.bytesRead), limits.RequestBytes+1)
		assert.Len(t, store.List(), 1)
	}
	var ticket capture.PreviewTicket
	for i := 0; i < limits.PendingPreviews; i++ {
		meta, err := store.Capture(context.Background(), "", []byte("queued"))
		require.NoError(t, err)
		ticket, err = store.BeginPreview(meta.ID)
		require.NoError(t, err)
	}
	w = submitPrint(router, []byte(`{"text":"capture despite full preview queue"}`))
	require.Equal(t, http.StatusOK, w.Code)
	newest := store.List()[0]
	_, err = store.BeginPreview(newest.ID)
	assert.ErrorIs(t, err, capture.ErrPreviewLimit)
	assert.Equal(t, "captured", newest.CaptureState)
	// A failed preview also leaves successful capture and readiness intact.
	queued := store.List()[1]
	require.ErrorIs(t, store.FinishPreview(ticket, nil, assert.AnError), assert.AnError)
	job, err := store.Get(queued.ID)
	require.NoError(t, err)
	assert.Equal(t, "failed", job.PreviewState)
	assert.Equal(t, "captured", job.CaptureState)
	assert.Equal(t, []byte("queued"), job.Payload)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/status", nil))
	assert.JSONEq(t, `{"reachable":true,"destination":"virtual","host":"","port":9100}`, w.Body.String())
}

func TestVirtualCORSRemainsConfigured(t *testing.T) {
	cfg := virtualConfig()
	cfg.AllowedOrigins = []string{"https://trusted.example"}
	store, router := virtualRouter(t, cfg)
	for _, tc := range []struct {
		origin string
		status int
	}{
		{"https://other.example", http.StatusForbidden},
		{"https://trusted.example", http.StatusOK},
	} {
		req := httptest.NewRequest(http.MethodPost, "/print", strings.NewReader(`{"text":"ok"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, tc.status, w.Code)
		if tc.status == http.StatusOK {
			assert.Equal(t, tc.origin, w.Header().Get("Access-Control-Allow-Origin"))
		}
	}
	assert.Len(t, store.List(), 1)
}

func TestVirtualListenerCapturesAcrossModeAndPortChanges(t *testing.T) {
	cfg := virtualConfig()
	cfg.HTTPPort = freePort(t)
	srv := New(cfg, nil)
	t.Cleanup(func() { assert.NoError(t, srv.Stop(context.Background())); srv.CaptureStore().Close() })
	require.NoError(t, srv.Start())
	client := &http.Client{Timeout: 2 * time.Second}
	send := func() string {
		t.Helper()
		response, err := client.Post("http://"+srv.Status().Address+"/print", "application/json", strings.NewReader(`{"text":"retained"}`))
		require.NoError(t, err)
		defer func() { _ = response.Body.Close() }()
		require.Equal(t, http.StatusOK, response.StatusCode)
		var result map[string]string
		require.NoError(t, json.NewDecoder(response.Body).Decode(&result))
		return result["jobId"]
	}
	firstID := send()
	require.NotEmpty(t, firstID)
	client.CloseIdleConnections()
	cfg.DestinationType = config.DestinationPhysical
	require.NoError(t, srv.Restart(cfg))
	response, err := client.Post("http://"+srv.Status().Address+"/print", "application/json", strings.NewReader(`{"text":"requires host again"}`))
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, response.StatusCode)
	_, err = io.Copy(io.Discard, response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	client.CloseIdleConnections()
	cfg.DestinationType = config.DestinationVirtual
	cfg.HTTPPort = freePort(t)
	require.NoError(t, srv.Restart(cfg))
	secondID := send()
	assert.NotEqual(t, firstID, secondID)
	job, err := srv.CaptureStore().Get(firstID)
	require.NoError(t, err)
	assert.Equal(t, []byte("retained"), job.Payload)
	assert.Len(t, srv.CaptureStore().List(), 2)
}

func virtualConfig() config.Config {
	cfg := config.Default()
	cfg.DestinationType = config.DestinationVirtual
	return cfg
}

// virtualRouter makes any attempt to dial or bypass the virtual store fail the test.
func virtualRouter(t *testing.T, cfg config.Config) (*capture.Store, *gin.Engine) {
	t.Helper()
	store := capture.NewStore()
	t.Cleanup(store.Close)
	unexpected := func(context.Context, string) error {
		t.Error("virtual mode invoked a physical destination")
		return assert.AnError
	}
	return store, NewRouter(Options{Config: cfg, CaptureStore: store,
		Destination: stubDestination{check: unexpected, send: func(ctx context.Context, target string, _ []byte) error { return unexpected(ctx, target) }},
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			t.Error("virtual mode dialed a printer")
			return nil, assert.AnError
		},
	})
}
