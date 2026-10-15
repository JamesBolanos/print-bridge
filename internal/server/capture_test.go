package server

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"print-bridge/internal/capture"
	"print-bridge/internal/config"
)

func TestCaptureDestinationReceivesOriginalDecodedText(t *testing.T) {
	s := capture.NewStore()
	t.Cleanup(s.Close)
	router := NewRouter(Options{Config: config.Default(), Destination: s,
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			t.Error("capture must not dial a printer")
			return nil, assert.AnError
		},
	})
	body := []byte(`{"printerHostname":"printer.local","text":" \r\n\t^XA^FDcafé^FS^XZ\u0000 "}`)
	w := submitPrint(router, body)
	assert.Equal(t, http.StatusOK, w.Code)
	jobs := s.List()
	require.Len(t, jobs, 1)
	job, err := s.Get(jobs[0].ID)
	require.NoError(t, err)
	assert.Equal(t, []byte(" \r\n\t^XA^FDcafé^FS^XZ\x00 "), job.Payload)
	assert.Equal(t, "printer.local:9100", job.Target)
}

func TestCaptureRequestBodyLimitBeforeDecoding(t *testing.T) {
	cases := []struct{ name, body string }{
		{"large text", `{"printerHostname":"p","text":"` + strings.Repeat("x", 1024) + `"}`},
		{"trailing data", `{"printerHostname":"p","text":"x"}` + strings.Repeat(" ", 1024)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := capture.NewStore()
			t.Cleanup(s.Close)
			router := NewRouter(Options{Config: config.Default(), Destination: limitedCapture{Store: s, limit: 128}})
			body := &countingReader{Reader: strings.NewReader(tc.body)}
			req := httptest.NewRequest(http.MethodPost, "/print", body)
			req.ContentLength = -1 // limits must work without a Content-Length header
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
			assert.LessOrEqual(t, body.bytesRead, 129)
			assert.Empty(t, s.List())
		})
	}
}

func TestCaptureDecodedPayloadLimitAndClosedStore(t *testing.T) {
	s := capture.NewStore()
	t.Cleanup(s.Close)
	router := NewRouter(Options{Config: config.Default(), Destination: s})
	limit := capture.DefaultLimits().JobBytes
	// JSON escaping expands each NUL to six bytes; the body budget accommodates
	// a payload at its decoded limit without confusing encoding size with data size.
	body, err := json.Marshal(PrintJobRequest{PrinterHostname: "p", Text: strings.Repeat("\x00", limit)})
	require.NoError(t, err)
	w := submitPrint(router, body)
	assert.Equal(t, http.StatusOK, w.Code)
	require.Len(t, s.List(), 1)
	assert.Equal(t, limit, s.List()[0].ByteCount)
	body, err = json.Marshal(PrintJobRequest{PrinterHostname: "p", Text: strings.Repeat("x", limit+1)})
	require.NoError(t, err)
	w = submitPrint(router, body)
	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	assert.Len(t, s.List(), 1)
	s.Close()
	w = submitPrint(router, []byte(`{"printerHostname":"p","text":"late"}`))
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Empty(t, s.List())
}

func TestCaptureStoreSurvivesListenerRestart(t *testing.T) {
	cfg := config.Default()
	cfg.HTTPPort = freePort(t)
	srv := New(cfg, nil)
	store := srv.CaptureStore()
	t.Cleanup(func() {
		assert.NoError(t, srv.Stop(context.Background()))
		store.Close()
	})
	require.NoError(t, srv.Start())
	meta, err := store.Capture(context.Background(), "printer.local:9100", []byte("retained"))
	require.NoError(t, err)
	ticket, err := store.BeginPreview(meta.ID)
	require.NoError(t, err)
	next := cfg
	next.HTTPPort = freePort(t)
	require.NoError(t, srv.Restart(next))
	assert.Same(t, store, srv.CaptureStore())
	job, err := store.Get(meta.ID)
	require.NoError(t, err)
	assert.Equal(t, []byte("retained"), job.Payload)
	assert.NoError(t, ticket.Context.Err())
	require.NoError(t, srv.Stop(context.Background()))
	assert.Len(t, store.List(), 1)
	store.Close() // the application exit hook, distinct from a listener stop
	assert.ErrorIs(t, ticket.Context.Err(), context.Canceled)
	assert.Empty(t, store.List())
}

// limitedCapture exercises HTTP boundary behavior with small test budgets.
type limitedCapture struct {
	*capture.Store
	limit int64
}

// RequestLimit overrides only the wire-body budget for streaming tests.
func (d limitedCapture) RequestLimit() int64 { return d.limit }

type countingReader struct {
	io.Reader
	bytesRead int
}

// Read tracks actual source reads, including MaxBytesReader's one-byte lookahead.
func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.bytesRead += n
	return n, err
}
