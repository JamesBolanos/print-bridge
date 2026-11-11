package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"print-bridge/internal/capture"
	"print-bridge/internal/config"
)

func TestVirtualTCPListenerCapturesExactBytesAfterClientCloses(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	type receivedJob struct {
		target  string
		payload []byte
	}
	received := make(chan receivedJob, 1)
	virtual := newVirtualTCPListener(listener, func(_ context.Context, target string, payload []byte) error {
		received <- receivedJob{target: target, payload: bytes.Clone(payload)}
		return nil
	}, func(string, string, string) {})
	defer func() { require.NoError(t, virtual.Stop()) }()

	payload := []byte("\x00\r\n^XA^FDcafé 世界^FS^XZ\n ")
	conn, err := net.Dial("tcp", listener.Addr().String())
	require.NoError(t, err)
	_, err = conn.Write(payload)
	require.NoError(t, err)
	require.NoError(t, conn.Close())

	select {
	case got := <-received:
		assert.Equal(t, payload, got.payload)
		assert.NotEmpty(t, got.target)
	case <-time.After(time.Second):
		t.Fatal("listener did not process the job after EOF")
	}
}

func TestVirtualTCPListenerRejectsOversizedAndEmptyJobs(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var received int
	results := make(chan string, 2)
	virtual := newVirtualTCPListener(listener, func(_ context.Context, _ string, payload []byte) error {
		if len(payload) == 0 {
			return errors.New("missing required field 'text'")
		}
		received++
		return nil
	}, func(_, outcome, detail string) {
		if outcome == "error" {
			results <- detail
		}
	})
	defer func() { require.NoError(t, virtual.Stop()) }()

	conn, err := net.Dial("tcp", listener.Addr().String())
	require.NoError(t, err)
	_, err = io.Copy(conn, strings.NewReader(strings.Repeat("x", virtual.jobLimit+1)))
	require.NoError(t, err)
	require.NoError(t, conn.Close())
	assert.Contains(t, <-results, capture.ErrTooLarge.Error())

	conn, err = net.Dial("tcp", listener.Addr().String())
	require.NoError(t, err)
	require.NoError(t, conn.Close())
	assert.Contains(t, <-results, "missing required field 'text'")
	assert.Zero(t, received)
}

func TestVirtualTCPListenerReadTimeout(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	results := make(chan string, 1)
	virtual := newVirtualTCPListenerWithTimeout(listener, func(context.Context, string, []byte) error {
		return errors.New("unexpected capture")
	}, func(_, outcome, detail string) {
		if outcome == "error" {
			results <- detail
		}
	}, 100*time.Millisecond)

	conn, err := net.Dial("tcp", listener.Addr().String())
	require.NoError(t, err)
	assert.Contains(t, <-results, "timeout")
	require.NoError(t, virtual.Stop())
	_ = conn.Close()
}

func TestVirtualTCPListenerStopClosesActiveConnections(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	virtual := newVirtualTCPListener(listener, func(context.Context, string, []byte) error {
		return nil
	}, func(string, string, string) {})
	conn, err := net.Dial("tcp", listener.Addr().String())
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		virtual.mu.Lock()
		defer virtual.mu.Unlock()
		return len(virtual.conns) == 1
	}, time.Second, time.Millisecond)

	require.NoError(t, virtual.Stop())
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(time.Second)))
	_, err = conn.Read(make([]byte, 1))
	assert.Error(t, err)
	_ = conn.Close()
}

func TestVirtualTCPListenerBoundsConcurrentClients(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	results := make(chan string, virtualTCPMaxConnections+1)
	virtual := newVirtualTCPListener(listener, func(context.Context, string, []byte) error {
		return nil
	}, func(_, outcome, detail string) {
		if outcome == "error" {
			results <- detail
		}
	})
	var clients []net.Conn
	defer func() {
		for _, client := range clients {
			_ = client.Close()
		}
		require.NoError(t, virtual.Stop())
	}()

	for range virtualTCPMaxConnections {
		conn, err := net.Dial("tcp", listener.Addr().String())
		require.NoError(t, err)
		clients = append(clients, conn)
	}
	require.Eventually(t, func() bool {
		virtual.mu.Lock()
		defer virtual.mu.Unlock()
		return len(virtual.conns) == virtualTCPMaxConnections
	}, time.Second, time.Millisecond)
	extra, err := net.Dial("tcp", listener.Addr().String())
	require.NoError(t, err)
	clients = append(clients, extra)
	select {
	case detail := <-results:
		assert.Equal(t, "virtual printer connection limit reached", detail)
	case <-time.After(time.Second):
		t.Fatal("listener did not reject a client beyond its concurrency limit")
	}
}

func TestVirtualTCPServerLifecycleAndSharedCaptureFlow(t *testing.T) {
	cfg := configForVirtualTCPTest()
	server := New(cfg, nil)
	server.tcpAddress = "127.0.0.1:0"
	cfg.HTTPPort = freePort(t)
	server.cfg = cfg
	require.NoError(t, server.Start())

	server.mu.Lock()
	tcpAddress := server.tcpIngest.listener.Addr().String()
	server.mu.Unlock()
	payload := []byte("^XA^FO20,20^FDtcp job^FS^XZ\x00")
	conn, err := net.Dial("tcp", tcpAddress)
	require.NoError(t, err)
	_, err = conn.Write(payload)
	require.NoError(t, err)
	require.NoError(t, conn.Close())

	require.Eventually(t, func() bool {
		return len(server.CaptureStore().List()) == 1
	}, time.Second, 10*time.Millisecond)
	job, err := server.CaptureStore().Get(server.CaptureStore().List()[0].ID)
	require.NoError(t, err)
	assert.Equal(t, payload, job.Payload)
	assert.Equal(t, "zpl", job.DetectedType)
	assert.Equal(t, "disabled", job.PreviewState)

	require.NoError(t, server.Stop(context.Background()))
	assert.Nil(t, server.tcpIngest)
	require.NoError(t, server.Close())
}

func TestVirtualTCPListenerOnlyStartsForEnabledVirtualDestination(t *testing.T) {
	for _, tc := range []struct {
		name        string
		destination string
		enabled     bool
		wantRunning bool
	}{
		{name: "physical mode", destination: config.DestinationPhysical, enabled: true},
		{name: "virtual disabled", destination: config.DestinationVirtual, enabled: false},
		{name: "virtual enabled", destination: config.DestinationVirtual, enabled: true, wantRunning: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := configForVirtualTCPTest()
			cfg.DestinationType = tc.destination
			cfg.VirtualTCPEnabled = tc.enabled
			cfg.HTTPPort = freePort(t)
			srv := New(cfg, nil)
			srv.tcpAddress = "127.0.0.1:0"
			require.NoError(t, srv.Start())
			srv.mu.Lock()
			running := srv.tcpIngest != nil
			srv.mu.Unlock()
			assert.Equal(t, tc.wantRunning, running)
			require.NoError(t, srv.Close())
		})
	}
}

func TestVirtualTCPPortConflictStopsHTTPStartup(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { require.NoError(t, occupied.Close()) }()

	cfg := configForVirtualTCPTest()
	cfg.HTTPPort = freePort(t)
	srv := New(cfg, nil)
	srv.tcpAddress = occupied.Addr().String()
	err = srv.Start()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "virtual printer listener")
	assert.False(t, srv.Status().Running)
	require.NoError(t, srv.Close())
}

func TestVirtualTCPListenerRejectsCaptureFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	results := make(chan string, 1)
	virtual := newVirtualTCPListener(listener, func(context.Context, string, []byte) error {
		return errors.New("capture unavailable")
	}, func(_, outcome, detail string) {
		if outcome == "error" {
			results <- detail
		}
	})
	defer func() { require.NoError(t, virtual.Stop()) }()

	conn, err := net.Dial("tcp", listener.Addr().String())
	require.NoError(t, err)
	_, err = conn.Write([]byte("job"))
	require.NoError(t, err)
	require.NoError(t, conn.Close())
	assert.Equal(t, "capture unavailable", <-results)
}

func configForVirtualTCPTest() config.Config {
	cfg := config.Default()
	cfg.DestinationType = config.DestinationVirtual
	cfg.VirtualTCPEnabled = true
	return cfg
}
