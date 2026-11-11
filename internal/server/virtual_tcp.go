package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"print-bridge/internal/capture"
)

const (
	virtualTCPMaxConnections = 8
	virtualTCPReadTimeout    = 30 * time.Second
)

type virtualTCPListener struct {
	listener    net.Listener
	receive     func(context.Context, string, []byte) error
	record      func(string, string, string)
	jobLimit    int
	readTimeout time.Duration
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
	slots       chan struct{}

	mu       sync.Mutex
	stopping bool
	conns    map[net.Conn]struct{}
	workers  sync.WaitGroup
}

// newVirtualTCPListener serves one raw print job per connection with bounded work.
func newVirtualTCPListener(listener net.Listener, receive func(context.Context, string, []byte) error, record func(string, string, string)) *virtualTCPListener {
	return newVirtualTCPListenerWithTimeout(listener, receive, record, virtualTCPReadTimeout)
}

// newVirtualTCPListenerWithTimeout exposes the read deadline for deterministic tests.
func newVirtualTCPListenerWithTimeout(listener net.Listener, receive func(context.Context, string, []byte) error, record func(string, string, string), readTimeout time.Duration) *virtualTCPListener {
	ctx, cancel := context.WithCancel(context.Background())
	server := &virtualTCPListener{
		listener:    listener,
		receive:     receive,
		record:      record,
		jobLimit:    capture.DefaultLimits().JobBytes,
		readTimeout: readTimeout,
		ctx:         ctx,
		cancel:      cancel,
		done:        make(chan struct{}),
		slots:       make(chan struct{}, virtualTCPMaxConnections),
		conns:       make(map[net.Conn]struct{}),
	}
	go server.serve()
	return server
}

// serve accepts clients until shutdown or a listener failure.
func (s *virtualTCPListener) serve() {
	defer func() {
		s.workers.Wait()
		close(s.done)
	}()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			s.mu.Lock()
			stopping := s.stopping
			if !stopping {
				s.stopping = true
				s.cancel()
				for active := range s.conns {
					_ = active.Close()
				}
			}
			s.mu.Unlock()
			if !stopping {
				s.record("", "error", fmt.Sprintf("accept print job: %v", err))
			}
			return
		}

		s.mu.Lock()
		if s.stopping {
			s.mu.Unlock()
			_ = conn.Close()
			continue
		}
		select {
		case s.slots <- struct{}{}:
			s.conns[conn] = struct{}{}
			s.workers.Add(1)
			s.mu.Unlock()
			go s.handle(conn)
		default:
			s.mu.Unlock()
			s.record(conn.RemoteAddr().String(), "error", "virtual printer connection limit reached")
			_ = conn.Close()
		}
	}
}

// handle reads one bounded job through EOF, then submits its exact bytes.
func (s *virtualTCPListener) handle(conn net.Conn) {
	defer s.workers.Done()
	defer func() {
		_ = conn.Close()
		<-s.slots
		s.mu.Lock()
		delete(s.conns, conn)
		s.mu.Unlock()
	}()

	target := conn.RemoteAddr().String()
	if err := conn.SetReadDeadline(time.Now().Add(s.readTimeout)); err != nil {
		s.record(target, "error", "set print job read timeout: "+err.Error())
		return
	}
	payload, err := io.ReadAll(io.LimitReader(conn, int64(s.jobLimit)+1))
	if err != nil {
		s.record(target, "error", "read print job: "+err.Error())
		return
	}
	if len(payload) > s.jobLimit {
		s.record(target, "error", capture.ErrTooLarge.Error())
		return
	}
	if err := s.receive(s.ctx, target, payload); err != nil {
		detail := err.Error()
		if errors.Is(err, capture.ErrTooLarge) {
			detail = capture.ErrTooLarge.Error()
		}
		s.record(target, "error", detail)
		return
	}
	s.record(target, "success", fmt.Sprintf("captured %d bytes", len(payload)))
}

// Stop closes the listener and active clients, then waits for their handlers.
func (s *virtualTCPListener) Stop() error {
	s.mu.Lock()
	if !s.stopping {
		s.stopping = true
		s.cancel()
		for conn := range s.conns {
			_ = conn.Close()
		}
	}
	s.mu.Unlock()

	err := s.listener.Close()
	if errors.Is(err, net.ErrClosed) {
		err = nil
	}
	<-s.done
	return err
}

func (s *virtualTCPListener) running() bool {
	select {
	case <-s.done:
		return false
	default:
		return true
	}
}
