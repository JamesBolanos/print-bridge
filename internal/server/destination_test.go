package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"print-bridge/internal/config"
	"print-bridge/internal/logging"
)

func TestPhysicalDestinationPreservesBytesAndDeadlines(t *testing.T) {
	for _, custom := range []bool{false, true} {
		name := "defaults"
		if custom {
			name = "custom settings"
		}
		t.Run(name, func(t *testing.T) {
			conn := &trackedConn{}
			cfg := config.Default()
			opts := Options{Config: cfg}
			connectTimeout, writeTimeout := DefaultConnectTimeout, DefaultWriteTimeout
			if custom {
				opts.Config.DefaultPrinterPort = 9200
				opts.ConnectTimeout = 2 * time.Second
				opts.WriteTimeout = 4 * time.Second
				connectTimeout, writeTimeout = opts.ConnectTimeout, opts.WriteTimeout
			}
			var connectDeadline time.Time
			var dialCtx context.Context
			opts.DialContext = func(ctx context.Context, network, target string) (net.Conn, error) {
				dialCtx = ctx
				var ok bool
				connectDeadline, ok = ctx.Deadline()
				assert.True(t, ok)
				assert.Equal(t, "tcp", network)
				assert.Equal(t, net.JoinHostPort("printer.local", strconv.Itoa(opts.Config.DefaultPrinterPort)), target)
				return conn, nil
			}
			// Include whitespace, CRLF, Unicode, NUL, and markup. No format parser
			// should interpret or normalize any of these bytes.
			payload := " \r\n\t^XA^FDcafé 世界^FS^XZ\x00<script>raw</script>\n "
			body, err := json.Marshal(PrintJobRequest{PrinterHostname: " printer.local ", Text: payload})
			require.NoError(t, err)
			router := NewRouter(opts)
			before := time.Now()
			w := submitPrint(router, body)
			after := time.Now()

			assert.Equal(t, http.StatusOK, w.Code)
			assert.JSONEq(t, `{"message":"OK"}`, w.Body.String())
			assert.Equal(t, []byte(payload), conn.buf.Bytes())
			assert.Equal(t, 1, conn.writes)
			assert.Equal(t, 1, conn.closes)
			assert.False(t, connectDeadline.Before(before.Add(connectTimeout)))
			assert.False(t, connectDeadline.After(after.Add(connectTimeout)))
			assert.False(t, conn.writeDeadline.Before(before.Add(writeTimeout)))
			assert.False(t, conn.writeDeadline.After(after.Add(writeTimeout)))
			require.NotNil(t, dialCtx)
			assert.ErrorIs(t, dialCtx.Err(), context.Canceled)
		})
	}
}

func TestPhysicalDestinationErrorsPreserveResponsesAndLogs(t *testing.T) {
	cases := []struct {
		name        string
		dialErr     error
		deadlineErr error
		writeErr    error
		status      int
		message     string
		logDetail   string
		writes      int
		closes      int
	}{
		{name: "connect", dialErr: errors.New("dial failed"), status: 502,
			message: "error connecting to printer: dial failed", logDetail: "error connecting to printer: dial failed"},
		{name: "deadline", deadlineErr: errors.New("deadline failed"), status: 500,
			message: "error sending data to printer: deadline failed", logDetail: "error setting write deadline: deadline failed", closes: 1},
		{name: "write", writeErr: errors.New("write failed"), status: 500,
			message: "error sending data to printer: write failed", logDetail: "error sending data to printer: write failed", writes: 1, closes: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn := &trackedConn{deadlineErr: tc.deadlineErr}
			conn.writeErr = tc.writeErr
			logger := logging.New(filepath.Join(t.TempDir(), "requests.log"), 1024, 1, nil)
			router := NewRouter(Options{Config: config.Default(), Logger: logger,
				DialContext: func(context.Context, string, string) (net.Conn, error) {
					return conn, tc.dialErr
				},
			})
			w := submitPrint(router, []byte(`{"printerHostname":"printer.local","text":"DATA"}`))
			assert.Equal(t, tc.status, w.Code)
			assert.Equal(t, tc.message, responseMessage(t, w))
			assert.Equal(t, tc.writes, conn.writes)
			assert.Equal(t, tc.closes, conn.closes)
			lines, err := logger.ReadLinesNewestFirst(1)
			require.NoError(t, err)
			require.Len(t, lines, 1)
			assert.Contains(t, lines[0], tc.logDetail)
		})
	}
}

func TestPhysicalDestinationConnectTimeout(t *testing.T) {
	for _, endpoint := range []string{"/print", "/status?host=printer.local"} {
		t.Run(endpoint, func(t *testing.T) {
			router := NewRouter(Options{Config: config.Default(), ConnectTimeout: 10 * time.Millisecond,
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					<-ctx.Done()
					assert.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
					return nil, ctx.Err()
				},
			})
			// A parent deadline also bounds a broken implementation in this test.
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			method := http.MethodGet
			if endpoint == "/print" {
				method = http.MethodPost
			}
			req := httptest.NewRequest(method, endpoint, strings.NewReader(`{"printerHostname":"printer.local","text":"DATA"}`)).WithContext(ctx)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			assert.NoError(t, ctx.Err(), "the transport timeout should expire before the request timeout")
			if method == http.MethodPost {
				assert.Equal(t, http.StatusBadGateway, w.Code)
				assert.Equal(t, "error connecting to printer: context deadline exceeded", responseMessage(t, w))
			} else {
				assert.Equal(t, http.StatusOK, w.Code)
				assert.JSONEq(t, `{"reachable":false,"host":"printer.local","port":9100}`, w.Body.String())
			}
		})
	}
}

func TestPhysicalDestinationWriteTimeout(t *testing.T) {
	writer, reader := net.Pipe()
	t.Cleanup(func() { _ = writer.Close(); _ = reader.Close() })
	// Nothing reads the pipe, so the real net.Conn deadline must unblock Write.
	// A watchdog prevents a regression from hanging the test process.
	watchdog := time.AfterFunc(2*time.Second, func() { _ = reader.Close() })
	defer watchdog.Stop()
	router := NewRouter(Options{Config: config.Default(), WriteTimeout: 10 * time.Millisecond,
		DialContext: func(context.Context, string, string) (net.Conn, error) { return writer, nil },
	})
	w := submitPrint(router, []byte(`{"printerHostname":"printer.local","text":"DATA"}`))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, responseMessage(t, w), "error sending data to printer:")
	assert.Contains(t, responseMessage(t, w), "i/o timeout")
}

func TestPhysicalDestinationStatusOnlyConnects(t *testing.T) {
	conn := &trackedConn{}
	router := testRouter(t, func(context.Context, string, string) (net.Conn, error) { return conn, nil })
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/status?host=printer.local", nil))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 0, conn.writes)
	assert.True(t, conn.writeDeadline.IsZero())
	assert.Equal(t, 1, conn.closes)
}

func TestDestinationDispatchNeverFallsBackToTCP(t *testing.T) {
	for _, failed := range []bool{false, true} {
		name := "success"
		if failed {
			name = "failure"
		}
		t.Run(name, func(t *testing.T) {
			sends, checks, dials := 0, 0, 0
			var result error
			if failed {
				result = errors.New("destination unavailable")
			}
			router := NewRouter(Options{Config: config.Default(),
				Destination: stubDestination{
					send: func(_ context.Context, target string, payload []byte) error {
						sends++
						assert.Equal(t, "printer.local:9100", target)
						assert.Equal(t, []byte("opaque data"), payload)
						return result
					},
					check: func(_ context.Context, target string) error {
						checks++
						assert.Equal(t, "printer.local:9100", target)
						return result
					},
				},
				DialContext: func(context.Context, string, string) (net.Conn, error) {
					dials++
					return nil, errors.New("unexpected physical fallback")
				},
			})
			w := submitPrint(router, []byte(`{"printerHostname":"printer.local","text":"opaque data"}`))
			if failed {
				assert.Equal(t, http.StatusInternalServerError, w.Code)
			} else {
				assert.Equal(t, http.StatusOK, w.Code)
			}
			w = httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/status?host=printer.local", nil))
			assert.Equal(t, http.StatusOK, w.Code)
			var status struct{ Reachable bool }
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &status))
			assert.Equal(t, !failed, status.Reachable)
			assert.Equal(t, 1, sends)
			assert.Equal(t, 1, checks)
			assert.Zero(t, dials)
		})
	}
}

func TestRouterKeepsDestinationForInFlightRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	opts := Options{Config: config.Default(), Destination: stubDestination{
		send: func(ctx context.Context, _ string, _ []byte) error {
			close(started)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}}
	oldRouter := NewRouter(opts)
	oldResponse := httptest.NewRecorder()
	go func() {
		defer close(finished)
		req := httptest.NewRequest(http.MethodPost, "/print", strings.NewReader(`{"printerHostname":"printer.local","text":"old job"}`)).WithContext(ctx)
		oldRouter.ServeHTTP(oldResponse, req)
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("request did not reach its destination")
	}
	newPayloads := []string{}
	opts.Destination = stubDestination{send: func(_ context.Context, _ string, payload []byte) error {
		newPayloads = append(newPayloads, string(payload))
		return nil
	}}
	newResponse := submitPrint(NewRouter(opts), []byte(`{"printerHostname":"printer.local","text":"new job"}`))
	close(release)
	select {
	case <-finished:
	case <-ctx.Done():
		t.Fatal("original request did not finish")
	}
	assert.Equal(t, http.StatusOK, oldResponse.Code)
	assert.Equal(t, http.StatusOK, newResponse.Code)
	assert.Equal(t, []string{"new job"}, newPayloads)
}

func submitPrint(router http.Handler, body []byte) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/print", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	return w
}

type stubDestination struct {
	send  func(context.Context, string, []byte) error
	check func(context.Context, string) error
}

func (d stubDestination) Send(ctx context.Context, target string, payload []byte) error {
	return d.send(ctx, target, payload)
}

func (d stubDestination) Check(ctx context.Context, target string) error {
	return d.check(ctx, target)
}

type trackedConn struct {
	captureConn
	deadlineErr   error
	writeDeadline time.Time
	writes        int
	closes        int
}

func (c *trackedConn) SetWriteDeadline(deadline time.Time) error {
	c.writeDeadline = deadline
	return c.deadlineErr
}

func (c *trackedConn) Write(payload []byte) (int, error) {
	c.writes++
	return c.captureConn.Write(payload)
}

func (c *trackedConn) Close() error {
	c.closes++
	return nil
}
