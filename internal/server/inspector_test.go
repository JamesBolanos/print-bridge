package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"print-bridge/internal/capture"
	"print-bridge/internal/config"
)

func TestInspectorServesBoundedMetadataAndOriginalPayloads(t *testing.T) {
	store := capture.NewStore()
	t.Cleanup(store.Close)
	cfg := config.Default()
	cfg.DestinationType = config.DestinationVirtual
	cfg.AllowedOrigins = []string{"https://print-client.example"}
	router := NewRouter(Options{Config: cfg, CaptureStore: store})
	payload := []byte("<script>raw</script>\r\n\x00")
	meta, err := store.Capture(context.Background(), "printer.local:9100", payload)
	require.NoError(t, err)

	w := inspectorRequest(router, http.MethodGet, "/api/inspector/jobs", "", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assertInspectorHeaders(t, w)
	var listing struct {
		Jobs []map[string]any `json:"jobs"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listing))
	require.Len(t, listing.Jobs, 1)
	assert.Equal(t, meta.ID, listing.Jobs[0]["id"])
	assert.Equal(t, float64(len(payload)), listing.Jobs[0]["byteCount"])
	assert.NotContains(t, w.Body.String(), string(payload))

	w = inspectorRequest(router, http.MethodGet, "/api/inspector/jobs/"+meta.ID, "", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assertInspectorHeaders(t, w)
	assert.Contains(t, w.Body.String(), `"downloadUrl":"/api/inspector/jobs/`)
	assert.NotContains(t, w.Body.String(), string(payload))

	w = inspectorRequest(router, http.MethodGet, "/api/inspector/jobs/"+meta.ID+"/download", "", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assertInspectorHeaders(t, w)
	assert.Equal(t, "application/octet-stream", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Header().Get("Content-Disposition"), "attachment")
	assert.True(t, bytes.Equal(payload, w.Body.Bytes()), "download must preserve payload bytes")

	ticket, err := store.BeginPreview(meta.ID)
	require.NoError(t, err)
	require.NoError(t, store.StartPreview(ticket))
	preview := []byte{137, 80, 78, 71, 13, 10, 26, 10, 0xff}
	require.NoError(t, store.FinishPreview(ticket, preview, nil))
	w = inspectorRequest(router, http.MethodGet, "/api/inspector/jobs/"+meta.ID+"/preview", "", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assertInspectorHeaders(t, w)
	assert.Equal(t, "image/png", w.Header().Get("Content-Type"))
	assert.Equal(t, preview, w.Body.Bytes())

	w = inspectorRequest(router, http.MethodGet, "/inspector/", "", "")
	require.Equal(t, http.StatusOK, w.Code)
	assertInspectorHeaders(t, w)
	assert.Contains(t, w.Body.String(), "PrintBridge Inspector")
	assert.NotContains(t, w.Body.String(), "https://")
	assert.Equal(t, "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'", w.Header().Get("Content-Security-Policy"))
	for _, resource := range []struct {
		path, contentType, marker string
	}{
		{"/inspector/app.js", "text/javascript; charset=utf-8", "textContent"},
		{"/inspector/styles.css", "text/css; charset=utf-8", "prefers-color-scheme"},
	} {
		w = inspectorRequest(router, http.MethodGet, resource.path, "", "")
		require.Equal(t, http.StatusOK, w.Code, resource.path)
		assertInspectorHeaders(t, w)
		assert.Equal(t, resource.contentType, w.Header().Get("Content-Type"))
		assert.Equal(t, "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'", w.Header().Get("Content-Security-Policy"))
		assert.Contains(t, w.Body.String(), resource.marker)
		assert.NotContains(t, w.Body.String(), "https://")
	}
}

func TestInspectorDashboardUsesSafeOfflineRendering(t *testing.T) {
	router := NewRouter(Options{Config: config.Default()})
	w := inspectorRequest(router, http.MethodGet, "/inspector/", "", "")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `src="/inspector/app.js"`)
	assert.Contains(t, w.Body.String(), `href="/inspector/styles.css"`)
	assert.NotContains(t, w.Body.String(), "<script>")

	script := inspectorRequest(router, http.MethodGet, "/inspector/app.js", "", "")
	require.Equal(t, http.StatusOK, script.Code)
	assert.Contains(t, script.Body.String(), "replaceChildren")
	assert.Contains(t, script.Body.String(), "textContent")
	assert.Contains(t, script.Body.String(), "Reopen Inspector from print-bridge after changing its port.")
	assert.NotContains(t, script.Body.String(), "innerHTML")
	assert.NotContains(t, script.Body.String(), "https://")
}

func TestInspectorFlowFromPrintSubmissionToOriginalDownload(t *testing.T) {
	store := capture.NewStore()
	t.Cleanup(store.Close)
	cfg := config.Default()
	cfg.DestinationType = config.DestinationVirtual
	router := NewRouter(Options{Config: cfg, CaptureStore: store})
	payload := " \r\n\t^XA^FD<script>not markup</script> 世界\x00^FS^XZ "
	submit := httptest.NewRequest(http.MethodPost, "/print", strings.NewReader(
		`{"text":" \r\n\t^XA^FD<script>not markup</script> 世界\u0000^FS^XZ "}`,
	))
	submit.Header.Set("Content-Type", "application/json")
	submitResult := httptest.NewRecorder()
	router.ServeHTTP(submitResult, submit)
	require.Equal(t, http.StatusOK, submitResult.Code, submitResult.Body.String())
	var response map[string]string
	require.NoError(t, json.Unmarshal(submitResult.Body.Bytes(), &response))
	require.NotEmpty(t, response["jobId"])

	list := inspectorRequest(router, http.MethodGet, "/api/inspector/jobs", "", "")
	require.Equal(t, http.StatusOK, list.Code, list.Body.String())
	assert.Contains(t, list.Body.String(), response["jobId"])
	assert.NotContains(t, list.Body.String(), "<script>")

	download := inspectorRequest(router, http.MethodGet, "/api/inspector/jobs/"+response["jobId"]+"/download", "", "")
	require.Equal(t, http.StatusOK, download.Code, download.Body.String())
	assert.Equal(t, []byte(payload), download.Body.Bytes())
}

func TestInspectorMissingAndUnavailableJobs(t *testing.T) {
	store := capture.NewStore()
	t.Cleanup(store.Close)
	cfg := config.Default()
	cfg.DestinationType = config.DestinationVirtual
	router := NewRouter(Options{Config: cfg, CaptureStore: store})

	w := inspectorRequest(router, http.MethodGet, "/api/inspector/jobs/missing", "", "")
	require.Equal(t, http.StatusNotFound, w.Code)
	assert.JSONEq(t, `{"message":"job not found"}`, w.Body.String())

	meta, err := store.Capture(context.Background(), "", []byte("job"))
	require.NoError(t, err)
	w = inspectorRequest(router, http.MethodGet, "/api/inspector/jobs/"+meta.ID+"/preview", "", "")
	require.Equal(t, http.StatusConflict, w.Code)
	assert.JSONEq(t, `{"message":"preview not available","previewState":"disabled"}`, w.Body.String())

	store.Clear()
	w = inspectorRequest(router, http.MethodGet, "/api/inspector/jobs/"+meta.ID+"/download", "", "")
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestInspectorRoutesRemainAvailableWhenPhysicalModeIsSelected(t *testing.T) {
	router := NewRouter(Options{Config: config.Default()})
	w := inspectorRequest(router, http.MethodGet, "/api/inspector/jobs", "", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.JSONEq(t, `{"jobs":[],"previewEnabled":false}`, w.Body.String())
}

func TestInspectorOriginHostAndFetchMetadataBoundaries(t *testing.T) {
	store := capture.NewStore()
	t.Cleanup(store.Close)
	cfg := config.Default()
	cfg.DestinationType = config.DestinationVirtual
	cfg.AllowedOrigins = []string{"https://print-client.example"}
	router := NewRouter(Options{Config: cfg, CaptureStore: store})

	for _, tc := range []struct {
		name, host, origin, fetchSite string
		want                          int
	}{
		{"same origin", "", testInspectorOrigin(cfg.HTTPPort), "same-origin", http.StatusOK},
		{"missing origin for navigation", "", "", "none", http.StatusOK},
		{"missing origin for local client", "", "", "", http.StatusOK},
		{"foreign origin", "", "https://evil.example", "cross-site", http.StatusForbidden},
		{"null origin", "", "null", "", http.StatusForbidden},
		{"allowed print origin remains forbidden", "", "https://print-client.example", "cross-site", http.StatusForbidden},
		{"cross-site fetch metadata", "", "", "cross-site", http.StatusForbidden},
		{"same-site is not same-origin", "", "", "same-site", http.StatusForbidden},
		{"unexpected host", "attacker.example:8080", testInspectorOrigin(cfg.HTTPPort), "same-origin", http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := inspectorRequest(router, http.MethodGet, "/api/inspector/jobs", tc.origin, tc.fetchSite)
			if tc.host != "" {
				w = inspectorRequestWithHost(router, http.MethodGet, "/api/inspector/jobs", tc.host, tc.origin, tc.fetchSite)
			}
			assert.Equal(t, tc.want, w.Code, w.Body.String())
			assertInspectorHeaders(t, w)
			assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"), "inspector does not use integration CORS")
		})
	}
}

func TestInspectorClearRequiresSameOriginAndDoesNotUseIntegrationCORS(t *testing.T) {
	store := capture.NewStore()
	t.Cleanup(store.Close)
	cfg := config.Default()
	cfg.DestinationType = config.DestinationVirtual
	cfg.AllowedOrigins = []string{"https://print-client.example"}
	router := NewRouter(Options{Config: cfg, CaptureStore: store})

	_, err := store.Capture(context.Background(), "", []byte("preserve"))
	require.NoError(t, err)
	for _, tc := range []struct {
		name, origin, fetchSite string
		want                    int
	}{
		{"missing origin", "", "", http.StatusForbidden},
		{"null origin", "null", "", http.StatusForbidden},
		{"foreign origin", "https://print-client.example", "cross-site", http.StatusForbidden},
		{"same origin", testInspectorOrigin(cfg.HTTPPort), "same-origin", http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := inspectorRequest(router, http.MethodDelete, "/api/inspector/jobs", tc.origin, tc.fetchSite)
			assert.Equal(t, tc.want, w.Code, w.Body.String())
			assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
			if tc.want == http.StatusOK {
				assert.JSONEq(t, `{"message":"OK"}`, w.Body.String())
			} else {
				assert.Len(t, store.List(), 1, "rejected requests must not mutate history")
			}
		})
	}
	assert.Empty(t, store.List())
}

func TestInspectorRoutesAreBoundedByStoreRetention(t *testing.T) {
	store := capture.NewStore()
	t.Cleanup(store.Close)
	cfg := config.Default()
	cfg.DestinationType = config.DestinationVirtual
	router := NewRouter(Options{Config: cfg, CaptureStore: store})
	limits := capture.DefaultLimits()
	for i := 0; i < limits.Jobs+1; i++ {
		_, err := store.Capture(context.Background(), "", []byte(strconv.Itoa(i)))
		require.NoError(t, err)
	}
	w := inspectorRequest(router, http.MethodGet, "/api/inspector/jobs", "", "")
	require.Equal(t, http.StatusOK, w.Code)
	var listing struct {
		Jobs []json.RawMessage `json:"jobs"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listing))
	assert.Len(t, listing.Jobs, limits.Jobs)
}

func inspectorRequest(router http.Handler, method, path, origin, fetchSite string) *httptest.ResponseRecorder {
	port := config.DefaultHTTPPort
	return inspectorRequestWithHost(router, method, path, net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), origin, fetchSite)
}

func inspectorRequestWithHost(router http.Handler, method, path, host, origin, fetchSite string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://127.0.0.1:8080"+path, nil)
	req.Host = host
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if fetchSite != "" {
		req.Header.Set("Sec-Fetch-Site", fetchSite)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func testInspectorOrigin(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

func assertInspectorHeaders(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", w.Header().Get("X-Frame-Options"))
	assert.Equal(t, "same-origin", w.Header().Get("Cross-Origin-Resource-Policy"))
	assert.Equal(t, "no-referrer", w.Header().Get("Referrer-Policy"))
}
