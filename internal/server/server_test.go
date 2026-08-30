package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"printer-bridge/internal/config"
)

func TestPing(t *testing.T) {
	router := testRouter(t, nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"message":"pong"}`, w.Body.String())
}

func TestEmptyAllowedOriginsDenyBrowserOrigin(t *testing.T) {
	router := testRouter(t, nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
}

func TestConfiguredAllowedOriginReceivesCORSHeader(t *testing.T) {
	cfg := config.Default()
	cfg.AllowedOrigins = []string{"http://localhost:3000"}
	router := testRouterWithConfig(t, cfg, nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "http://localhost:3000", w.Header().Get("Access-Control-Allow-Origin"))
}

func TestPrintDefaultsToConfiguredPrinterPort(t *testing.T) {
	conn := &captureConn{}
	var dialed string
	router := testRouter(t, func(ctx context.Context, network string, address string) (net.Conn, error) {
		dialed = address
		return conn, nil
	})

	body := `{"printerHostname":"192.168.1.100","text":"^XA^XZ"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/print", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "192.168.1.100:9100", dialed)
	assert.Equal(t, "^XA^XZ", conn.buf.String())
	assert.JSONEq(t, `{"message":"OK"}`, w.Body.String())
}

func TestPrintUsesRequestPrinterPort(t *testing.T) {
	conn := &captureConn{}
	var dialed string
	router := testRouter(t, func(ctx context.Context, network string, address string) (net.Conn, error) {
		dialed = address
		return conn, nil
	})

	body := `{"printerHostname":"printer.local","printerPort":9101,"text":"DATA"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/print", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "printer.local:9101", dialed)
	assert.Equal(t, "DATA", conn.buf.String())
}

func TestPrintInvalidJSON(t *testing.T) {
	router := testRouter(t, nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/print", strings.NewReader("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, responseMessage(t, w), "error parsing JSON")
}

func TestPrintMissingRequiredFields(t *testing.T) {
	router := testRouter(t, nil)

	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "missing host", body: `{"text":"DATA"}`, want: "printerHostname"},
		{name: "missing text", body: `{"printerHostname":"127.0.0.1"}`, want: "text"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/print", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, responseMessage(t, w), tt.want)
		})
	}
}

func TestPrintConnectionFailureReturnsBadGateway(t *testing.T) {
	router := testRouter(t, func(ctx context.Context, network string, address string) (net.Conn, error) {
		return nil, errors.New("dial failed")
	})

	body := `{"printerHostname":"printer.local","text":"DATA"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/print", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadGateway, w.Code)
	assert.Contains(t, responseMessage(t, w), "error connecting to printer")
}

func TestStatusReachable(t *testing.T) {
	router := testRouter(t, func(ctx context.Context, network string, address string) (net.Conn, error) {
		return &captureConn{}, nil
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/status?host=printer.local&port=9101", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"reachable":true,"host":"printer.local","port":9101}`, w.Body.String())
}

func TestStatusUnreachableStillReturnsOK(t *testing.T) {
	router := testRouter(t, func(ctx context.Context, network string, address string) (net.Conn, error) {
		return nil, errors.New("connection refused")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/status?host=printer.local", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"reachable":false,"host":"printer.local","port":9100}`, w.Body.String())
}

func TestStatusRequiresHost(t *testing.T) {
	router := testRouter(t, nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, responseMessage(t, w), "host")
}

func TestServerBindsToLocalhost(t *testing.T) {
	cfg := config.Default()
	cfg.HTTPPort = freePort(t)

	srv := New(cfg, nil)
	require.NoError(t, srv.Start())

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	defer srv.Stop(ctx)

	status := srv.Status()
	assert.True(t, status.Running)
	assert.Equal(t, net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.HTTPPort)), status.Address)
}

func testRouter(t *testing.T, dial DialContextFunc) *gin.Engine {
	t.Helper()

	return testRouterWithConfig(t, config.Default(), dial)
}

func testRouterWithConfig(t *testing.T, cfg config.Config, dial DialContextFunc) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	opts := Options{Config: cfg, DialContext: dial}
	return NewRouter(opts)
}

func responseMessage(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()

	var response map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	return response["message"]
}

func freePort(t *testing.T) int {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	_, portString, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)

	port, err := strconv.Atoi(portString)
	require.NoError(t, err)
	return port
}

type captureConn struct {
	buf bytes.Buffer
}

func (c *captureConn) Read(b []byte) (int, error)         { return 0, errors.New("not implemented") }
func (c *captureConn) Write(b []byte) (int, error)        { return c.buf.Write(b) }
func (c *captureConn) Close() error                       { return nil }
func (c *captureConn) LocalAddr() net.Addr                { return dummyAddr("local") }
func (c *captureConn) RemoteAddr() net.Addr               { return dummyAddr("remote") }
func (c *captureConn) SetDeadline(t time.Time) error      { return nil }
func (c *captureConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *captureConn) SetWriteDeadline(t time.Time) error { return nil }

type dummyAddr string

func (a dummyAddr) Network() string { return string(a) }
func (a dummyAddr) String() string  { return string(a) }
