// Package server provides the localhost HTTP API that browser apps use to
// reach network printers or capture jobs in session memory.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"print-bridge/internal/activity"
	"print-bridge/internal/capture"
	"print-bridge/internal/config"
	"print-bridge/internal/logging"
)

const (
	// DefaultConnectTimeout limits how long the bridge waits to reach a printer.
	DefaultConnectTimeout = 3 * time.Second
	// DefaultWriteTimeout limits how long the bridge waits while sending print data.
	DefaultWriteTimeout = 5 * time.Second
)

// DialContextFunc is the TCP dialing shape used by the server.
//
// Tests can provide a fake dialer so HTTP behavior can be exercised without a
// real network printer.
type DialContextFunc func(ctx context.Context, network string, address string) (net.Conn, error)

// Options configures an HTTP router instance.
type Options struct {
	Config         config.Config
	Logger         *logging.Logger
	ConnectTimeout time.Duration
	WriteTimeout   time.Duration
	DialContext    DialContextFunc
	// Destination overrides the physical transport only. Virtual mode always
	// uses CaptureStore, so an injected transport cannot bypass the saved mode.
	Destination Destination
	// CaptureStore owns virtual jobs across listener restarts. If nil, NewRouter
	// creates a store for its lifetime; application callers should supply one.
	CaptureStore *capture.Store
	previews     *previewManager
}

// PrintJobRequest is the JSON payload accepted by POST /print.
type PrintJobRequest struct {
	PrinterHostname string `json:"printerHostname"`
	PrinterPort     int    `json:"printerPort"`
	Text            string `json:"text"`
}

// Status describes the local listener state.
type Status struct {
	Running bool
	Address string
	Error   string
}

// Server owns the localhost HTTP listener and its lifecycle.
type Server struct {
	mu         sync.Mutex
	cfg        config.Config
	logger     *logging.Logger
	httpServer *http.Server
	listener   net.Listener
	lastErr    error
	captures   *capture.Store
	previews   *previewManager
}

// New creates a server that can be started with Start.
func New(cfg config.Config, logger *logging.Logger) *Server {
	captures := capture.NewStore()
	return &Server{
		cfg: cfg, logger: logger, captures: captures,
		previews: newPreviewManager(captures, nil),
	}
}

// CaptureStore returns the application session's store. Listener restarts keep
// it alive; Close releases it when the application exits.
func (s *Server) CaptureStore() *capture.Store { return s.captures }

// NewRouter builds the Gin router for the local print bridge API.
func NewRouter(opts Options) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())
	var err error
	opts, err = normalizeOptions(opts)
	if err != nil {
		// Invalid settings must never select the physical fallback.
		router.NoRoute(func(c *gin.Context) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"message": err.Error()})
		})
		return router
	}
	if opts.previews == nil {
		opts.previews = newPreviewManager(opts.CaptureStore, nil)
	}
	opts.previews.Configure(opts.Config.Preview, opts.Config.DestinationType == config.DestinationVirtual)
	corsMiddleware := cors.New(corsConfig(opts.Config.AllowedOrigins))
	router.Use(func(c *gin.Context) {
		if isInspectorRequest(c.Request.URL.Path) {
			c.Next()
			return
		}
		corsMiddleware(c)
	})

	router.GET("/ping", func(c *gin.Context) {
		record(opts, "/ping", "", "success", "pong")
		c.JSON(http.StatusOK, gin.H{"message": "pong"})
	})
	router.GET("/status", func(c *gin.Context) {
		handleStatus(c, opts)
	})
	router.POST("/print", func(c *gin.Context) {
		handlePrint(c, opts)
	})
	registerInspectorRoutes(router, opts)

	return router
}

func corsConfig(allowedOrigins []string) cors.Config {
	cfg := cors.Config{
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodOptions},
		AllowHeaders: []string{"Origin", "Content-Type"},
	}
	if len(allowedOrigins) == 0 {
		cfg.AllowOriginFunc = func(_ string) bool {
			return false
		}
		return cfg
	}

	cfg.AllowOrigins = allowedOrigins
	return cfg
}

// Start binds the HTTP listener to localhost and serves requests in a goroutine.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.httpServer != nil {
		return nil
	}

	normalized, err := config.Normalize(s.cfg)
	if err != nil {
		s.previews.Configure(config.PreviewConfig{}, false)
		s.lastErr = err
		return err
	}
	s.cfg = normalized
	s.previews.Configure(normalized.Preview, normalized.DestinationType == config.DestinationVirtual)

	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(s.cfg.HTTPPort))
	listener, err := net.Listen("tcp", address)
	if err != nil {
		s.lastErr = err
		return err
	}

	srv := &http.Server{
		Addr:    address,
		Handler: NewRouter(Options{Config: s.cfg, Logger: s.logger, CaptureStore: s.captures, previews: s.previews}),
	}
	ready := make(chan struct{})
	serveListener := &readyListener{Listener: listener, ready: ready}

	s.listener = listener
	s.httpServer = srv
	s.lastErr = nil

	go func() {
		err := srv.Serve(serveListener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.mu.Lock()
			if s.httpServer == srv {
				s.lastErr = err
				s.httpServer = nil
				s.listener = nil
			}
			s.mu.Unlock()
		}
	}()
	<-ready

	return nil
}

// Stop gracefully shuts down the HTTP listener.
func (s *Server) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	srv := s.httpServer
	s.httpServer = nil
	s.listener = nil
	s.mu.Unlock()

	if srv == nil {
		return nil
	}
	return srv.Shutdown(ctx)
}

type readyListener struct {
	net.Listener
	ready chan struct{}
	once  sync.Once
}

func (l *readyListener) Accept() (net.Conn, error) {
	l.once.Do(func() { close(l.ready) })
	return l.Listener.Accept()
}

// Restart replaces the server configuration and starts a fresh listener.
func (s *Server) Restart(cfg config.Config) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := s.Stop(ctx); err != nil {
		return err
	}

	s.mu.Lock()
	s.cfg = cfg
	s.mu.Unlock()

	return s.Start()
}

// Close stops the listener and releases preview workers and session captures.
func (s *Server) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := s.Stop(ctx)
	s.previews.Close()
	s.captures.Close()
	return err
}

// Status reports whether the listener is running and where it should be found.
func (s *Server) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()

	status := Status{
		Running: s.httpServer != nil,
		Address: net.JoinHostPort(
			"127.0.0.1",
			strconv.Itoa(s.cfg.HTTPPort),
		),
	}
	if s.lastErr != nil {
		status.Error = s.lastErr.Error()
	}
	return status
}

// normalizeOptions validates routing settings before creating any transport.
func normalizeOptions(opts Options) (Options, error) {
	normalized, err := config.Normalize(opts.Config)
	if err != nil {
		return opts, err
	}
	opts.Config = normalized
	if opts.ConnectTimeout <= 0 {
		opts.ConnectTimeout = DefaultConnectTimeout
	}
	if opts.WriteTimeout <= 0 {
		opts.WriteTimeout = DefaultWriteTimeout
	}
	if opts.DialContext == nil {
		dialer := &net.Dialer{}
		opts.DialContext = dialer.DialContext
	}
	if opts.CaptureStore == nil {
		opts.CaptureStore = capture.NewStore()
	}
	if opts.Config.DestinationType == config.DestinationVirtual {
		opts.Destination = opts.CaptureStore
		return opts, nil
	}
	if opts.Destination == nil {
		opts.Destination = tcpDestination{
			dial:           opts.DialContext,
			connectTimeout: opts.ConnectTimeout,
			writeTimeout:   opts.WriteTimeout,
		}
	}
	return opts, nil
}

// handleStatus probes only Physical targets; Virtual readiness is local and
// independent of preview state. Supplied addresses remain response metadata.
func handleStatus(c *gin.Context, opts Options) {
	host := strings.TrimSpace(c.Query("host"))
	port, err := parsePort(c.Query("port"), opts.Config.DefaultPrinterPort)
	target := ""
	if host != "" && err == nil {
		target = net.JoinHostPort(host, strconv.Itoa(port))
	}

	if host == "" && opts.Config.DestinationType == config.DestinationPhysical {
		record(opts, "/status", "", "error", "missing host query parameter")
		c.JSON(http.StatusBadRequest, gin.H{"message": "missing required query parameter 'host'"})
		return
	}
	if err != nil {
		record(opts, "/status", target, "error", err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	response := gin.H{"reachable": true, "host": host, "port": port}
	if opts.Config.DestinationType == config.DestinationVirtual {
		response["destination"] = config.DestinationVirtual
	}
	if err := opts.Destination.Check(c.Request.Context(), target); err != nil {
		record(opts, "/status", target, "unreachable", err.Error())
		response["reachable"] = false
	} else {
		record(opts, "/status", target, "reachable", "")
	}
	c.JSON(http.StatusOK, response)
}

// handlePrint validates against the router's fixed destination and acknowledges
// either an exact-byte socket write or a completed capture, never a preview.
func handlePrint(c *gin.Context, opts Options) {
	var req PrintJobRequest
	// Only bounded destinations opt into request limits; physical integrations
	// keep their existing behavior. Limit reads before the JSON decoder allocates.
	limited, bounded := opts.Destination.(interface{ RequestLimit() int64 })
	if bounded {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limited.RequestLimit())
	}
	virtual := opts.Config.DestinationType == config.DestinationVirtual
	var err error
	if virtual {
		err = decodeVirtualPrint(c.Request.Body, &req)
	} else {
		err = c.ShouldBindJSON(&req)
	}
	if err == nil && bounded && !virtual {
		// Account for trailing bytes too, including bodies with unknown length.
		_, err = io.Copy(io.Discard, c.Request.Body)
	}
	if err != nil {
		var sizeErr *http.MaxBytesError
		if errors.As(err, &sizeErr) {
			record(opts, "/print", "", "error", "print request exceeds size limit")
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"message": "print request exceeds size limit"})
			return
		}
		record(opts, "/print", "", "error", fmt.Sprintf("error parsing JSON: %v", err))
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Sprintf("error parsing JSON: %v", err)})
		return
	}

	req.PrinterHostname = strings.TrimSpace(req.PrinterHostname)
	if req.PrinterHostname == "" && !virtual {
		record(opts, "/print", "", "error", "missing required field 'printerHostname'")
		c.JSON(http.StatusBadRequest, gin.H{"message": "missing required field 'printerHostname'"})
		return
	}
	if req.Text == "" {
		record(opts, "/print", "", "error", "missing required field 'text'")
		c.JSON(http.StatusBadRequest, gin.H{"message": "missing required field 'text'"})
		return
	}

	port := req.PrinterPort
	if port == 0 {
		port = opts.Config.DefaultPrinterPort
	}
	if port < 1 || port > 65535 {
		record(opts, "/print", req.PrinterHostname, "error", "printerPort must be between 1 and 65535")
		c.JSON(http.StatusBadRequest, gin.H{"message": "printerPort must be between 1 and 65535"})
		return
	}

	target := net.JoinHostPort(req.PrinterHostname, strconv.Itoa(port))
	response := gin.H{"message": "OK"}
	if virtual {
		var meta capture.Metadata
		meta, err = opts.CaptureStore.Capture(c.Request.Context(), target, []byte(req.Text))
		if err == nil {
			detectedType := detectZPL([]byte(req.Text))
			previewState := "disabled"
			renderEnabled := opts.previews.Enabled()
			if renderEnabled && detectedType != "zpl" {
				previewState = "unsupported"
			}
			if classifyErr := opts.CaptureStore.Classify(meta.ID, detectedType, previewState); classifyErr == nil &&
				renderEnabled && detectedType == "zpl" {
				_ = opts.previews.Schedule(meta.ID, 0)
			}
		}
		response["destination"] = config.DestinationVirtual
		response["jobId"] = meta.ID
	} else {
		err = opts.Destination.Send(c.Request.Context(), target, []byte(req.Text))
	}
	if err != nil {
		status := http.StatusInternalServerError
		message := fmt.Sprintf("error sending data to destination: %v", err)
		if errors.Is(err, capture.ErrTooLarge) {
			status = http.StatusRequestEntityTooLarge
		} else if virtual || errors.Is(err, capture.ErrClosed) {
			status = http.StatusServiceUnavailable
		}
		var transportErr *tcpError
		if errors.As(err, &transportErr) {
			message = transportErr.Error()
			switch transportErr.operation {
			case "connecting to printer":
				status = http.StatusBadGateway
			case "setting write deadline":
				message = fmt.Sprintf("error sending data to printer: %v", transportErr.err)
			}
		}
		record(opts, "/print", target, "error", err.Error())
		c.JSON(status, gin.H{"message": message})
		return
	}

	record(opts, "/print", target, "success", "OK")
	c.JSON(http.StatusOK, response)
}

// decodeVirtualPrint accepts one JSON object and validates supplied field types.
// Reading through EOF rejects trailing documents and enforces the wire limit,
// including bytes buffered by the decoder. Unknown fields remain compatible
// with existing clients; they cannot override the configured destination.
func decodeVirtualPrint(body io.Reader, req *PrintJobRequest) error {
	decoder := json.NewDecoder(body)
	var fields map[string]json.RawMessage
	if err := decoder.Decode(&fields); err != nil {
		return err
	}
	if fields == nil {
		return errors.New("print request must be a JSON object")
	}
	for _, field := range []struct {
		name  string
		value any
	}{
		{"printerHostname", &req.PrinterHostname},
		{"printerPort", &req.PrinterPort},
		{"text", &req.Text},
	} {
		if raw, ok := fields[field.name]; ok {
			if string(raw) == "null" {
				return fmt.Errorf("field %q must not be null", field.name)
			}
			if err := json.Unmarshal(raw, field.value); err != nil {
				return err
			}
		}
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return err
		}
		return errors.New("print request must contain a single JSON object")
	}
	return nil
}

func parsePort(raw string, defaultPort int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultPort, nil
	}

	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("port must be between 1 and 65535")
	}
	return port, nil
}

func record(opts Options, endpoint string, target string, outcome string, detail string) {
	if opts.Logger == nil {
		return
	}
	opts.Logger.Record(activity.Entry{
		Timestamp: time.Now(),
		Endpoint:  endpoint,
		Target:    target,
		Outcome:   outcome,
		Detail:    detail,
	})
}
