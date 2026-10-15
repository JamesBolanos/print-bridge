package server

import (
	"context"
	"fmt"
	"time"
)

// Destination receives opaque print bytes and checks whether a target is ready.
// Implementations must be safe for concurrent requests. Neither method may
// fall back to another destination after an error.
type Destination interface {
	Send(ctx context.Context, target string, payload []byte) error
	Check(ctx context.Context, target string) error
}

// tcpDestination forwards bytes without inspecting their format. Its settings
// are fixed for the lifetime of the router that owns it.
type tcpDestination struct {
	dial           DialContextFunc
	connectTimeout time.Duration
	writeTimeout   time.Duration
}

func (d tcpDestination) Send(ctx context.Context, target string, payload []byte) error {
	ctx, cancel := context.WithTimeout(ctx, d.connectTimeout)
	defer cancel()

	conn, err := d.dial(ctx, "tcp", target)
	if err != nil {
		return &tcpError{operation: "connecting to printer", err: err}
	}
	defer func() { _ = conn.Close() }()

	if err := conn.SetWriteDeadline(time.Now().Add(d.writeTimeout)); err != nil {
		return &tcpError{operation: "setting write deadline", err: err}
	}
	if _, err := conn.Write(payload); err != nil {
		return &tcpError{operation: "sending data to printer", err: err}
	}
	return nil
}

func (d tcpDestination) Check(ctx context.Context, target string) error {
	ctx, cancel := context.WithTimeout(ctx, d.connectTimeout)
	defer cancel()

	conn, err := d.dial(ctx, "tcp", target)
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
}

// tcpError lets the HTTP layer retain the existing response and log messages
// without making the transport depend on HTTP status codes.
type tcpError struct {
	operation string
	err       error
}

func (e *tcpError) Error() string {
	return fmt.Sprintf("error %s: %v", e.operation, e.err)
}

func (e *tcpError) Unwrap() error { return e.err }
