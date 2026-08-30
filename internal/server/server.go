package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"printer-bridge/internal/activity"
	"printer-bridge/internal/config"
	"printer-bridge/internal/logging"
)

const (
	DefaultConnectTimeout = 3 * time.Second
	DefaultWriteTimeout   = 5 * time.Second
)

type DialContextFunc func(ctx context.Context, network string, address string) (net.Conn, error)

type Options struct {
	Config         config.Config
	Logger         *logging.Logger
	ConnectTimeout time.Duration
	WriteTimeout   time.Duration
	DialContext    DialContextFunc
}

type PrintJobRequest struct {
	PrinterHostname string `json:"printerHostname"`
	PrinterPort     int    `json:"printerPort"`
	Text            string `json:"text"`
}

type Status struct {
	Running bool
	Address string
	Error   string
}

type Server struct {
	mu         sync.Mutex
	cfg        config.Config
	logger     *logging.Logger
	httpServer *http.Server
	listener   net.Listener
	lastErr    error
}

func New(cfg config.Config, logger *logging.Logger) *Server {
	return &Server{cfg: cfg, logger: logger}
}

func NewRouter(opts Options) *gin.Engine {
	opts = normalizeOptions(opts)

	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(cors.New(cors.Config{
		AllowOrigins: opts.Config.AllowedOrigins,
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodOptions},
		AllowHeaders: []string{"Origin", "Content-Type"},
	}))

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

	return router
}

func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.httpServer != nil {
		return nil
	}

	normalized, err := config.Normalize(s.cfg)
	if err != nil {
		s.lastErr = err
		return err
	}
	s.cfg = normalized

	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(s.cfg.HTTPPort))
	listener, err := net.Listen("tcp", address)
	if err != nil {
		s.lastErr = err
		return err
	}

	srv := &http.Server{
		Addr:    address,
		Handler: NewRouter(Options{Config: s.cfg, Logger: s.logger}),
	}

	s.listener = listener
	s.httpServer = srv
	s.lastErr = nil

	go func() {
		err := srv.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.mu.Lock()
			s.lastErr = err
			s.httpServer = nil
			s.listener = nil
			s.mu.Unlock()
		}
	}()

	return nil
}

func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	srv := s.httpServer
	s.httpServer = nil
	s.listener = nil
	s.mu.Unlock()

	if srv == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return srv.Shutdown(ctx)
}

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

func normalizeOptions(opts Options) Options {
	normalized, err := config.Normalize(opts.Config)
	if err == nil {
		opts.Config = normalized
	}
	if opts.Config.DefaultPrinterPort == 0 {
		opts.Config.DefaultPrinterPort = config.DefaultPrinterPort
	}
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
	return opts
}

func handleStatus(c *gin.Context, opts Options) {
	host := strings.TrimSpace(c.Query("host"))
	port, err := parsePort(c.Query("port"), opts.Config.DefaultPrinterPort)
	target := ""
	if host != "" && err == nil {
		target = net.JoinHostPort(host, strconv.Itoa(port))
	}

	if host == "" {
		record(opts, "/status", "", "error", "missing host query parameter")
		c.JSON(http.StatusBadRequest, gin.H{"message": "missing required query parameter 'host'"})
		return
	}
	if err != nil {
		record(opts, "/status", target, "error", err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), opts.ConnectTimeout)
	defer cancel()

	conn, err := opts.DialContext(ctx, "tcp", target)
	if err != nil {
		record(opts, "/status", target, "unreachable", err.Error())
		c.JSON(http.StatusOK, gin.H{"reachable": false, "host": host, "port": port})
		return
	}
	_ = conn.Close()

	record(opts, "/status", target, "reachable", "")
	c.JSON(http.StatusOK, gin.H{"reachable": true, "host": host, "port": port})
}

func handlePrint(c *gin.Context, opts Options) {
	var req PrintJobRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		record(opts, "/print", "", "error", fmt.Sprintf("error parsing JSON: %v", err))
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Sprintf("error parsing JSON: %v", err)})
		return
	}

	req.PrinterHostname = strings.TrimSpace(req.PrinterHostname)
	if req.PrinterHostname == "" {
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
	ctx, cancel := context.WithTimeout(c.Request.Context(), opts.ConnectTimeout)
	defer cancel()

	conn, err := opts.DialContext(ctx, "tcp", target)
	if err != nil {
		record(opts, "/print", target, "error", fmt.Sprintf("error connecting to printer: %v", err))
		c.JSON(http.StatusBadGateway, gin.H{"message": fmt.Sprintf("error connecting to printer: %v", err)})
		return
	}
	defer conn.Close()

	if err := conn.SetWriteDeadline(time.Now().Add(opts.WriteTimeout)); err != nil {
		record(opts, "/print", target, "error", fmt.Sprintf("error setting write deadline: %v", err))
		c.JSON(http.StatusInternalServerError, gin.H{"message": fmt.Sprintf("error sending data to printer: %v", err)})
		return
	}
	if _, err := conn.Write([]byte(req.Text)); err != nil {
		record(opts, "/print", target, "error", fmt.Sprintf("error sending data to printer: %v", err))
		c.JSON(http.StatusInternalServerError, gin.H{"message": fmt.Sprintf("error sending data to printer: %v", err)})
		return
	}

	record(opts, "/print", target, "success", "OK")
	c.JSON(http.StatusOK, gin.H{"message": "OK"})
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
