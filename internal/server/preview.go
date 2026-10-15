package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"print-bridge/internal/capture"
	"print-bridge/internal/config"
)

const (
	previewWorkers        = 2
	previewQueueSize      = 8
	previewRequestTimeout = 15 * time.Second
	maxPreviewResponse    = 4 << 20
	maxPreviewImageSide   = 2000
	maxPreviewImagePixels = 4_000_000
	maxLabelIndex         = 99
)

var (
	errPreviewDisabled    = errors.New("preview rendering is disabled")
	errPreviewUnsupported = errors.New("job is not recognized as ZPL")
	errPreviewQueueFull   = errors.New("preview queue is full")
)

type previewRenderer interface {
	Render(context.Context, []byte, config.PreviewConfig, int) ([]byte, error)
}

type previewTask struct {
	ticket   capture.PreviewTicket
	payload  []byte
	settings config.PreviewConfig
	index    int
}

type previewManager struct {
	mu       sync.Mutex
	store    *capture.Store
	renderer previewRenderer
	queue    chan previewTask
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	started  bool
	enabled  bool
	settings config.PreviewConfig
	closed   bool
}

func newPreviewManager(store *capture.Store, renderer previewRenderer) *previewManager {
	ctx, cancel := context.WithCancel(context.Background())
	if renderer == nil {
		renderer = labelaryRenderer{client: &http.Client{Timeout: previewRequestTimeout}}
	}
	return &previewManager{
		store: store, renderer: renderer, queue: make(chan previewTask, previewQueueSize),
		ctx: ctx, cancel: cancel,
	}
}

// Configure enables only Virtual-mode preview rendering and cancels admitted
// work when rendering is disabled or the destination changes.
func (m *previewManager) Configure(settings config.PreviewConfig, virtual bool) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	enabled := virtual && settings.LabelaryEnabled
	wasEnabled := m.enabled
	renderSettingsChanged := m.settings.LabelWidthInches != settings.LabelWidthInches ||
		m.settings.LabelHeightInches != settings.LabelHeightInches ||
		m.settings.PrintDensityDpmm != settings.PrintDensityDpmm
	m.enabled = enabled
	m.settings = settings
	m.mu.Unlock()
	if renderSettingsChanged {
		m.store.InvalidatePreviews()
	} else if wasEnabled && !enabled {
		m.store.DisablePendingPreviews()
	}
}

func (m *previewManager) Enabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.enabled && !m.closed
}

// Schedule deduplicates active or already-cached requests before reserving
// bounded store work and placing one immutable payload snapshot on the queue.
func (m *previewManager) Schedule(id string, index int) error {
	if index < 0 || index > maxLabelIndex {
		return fmt.Errorf("label index must be between 0 and %d", maxLabelIndex)
	}
	m.mu.Lock()
	if !m.enabled || m.closed {
		m.mu.Unlock()
		return errPreviewDisabled
	}
	settings := m.settings
	m.mu.Unlock()

	job, err := m.store.Get(id)
	if err != nil {
		return err
	}
	if job.DetectedType != "zpl" {
		return errPreviewUnsupported
	}
	if job.PreviewLabelIndex == index {
		switch job.PreviewState {
		case "queued", "rendering", "ready":
			return nil
		}
	}
	ticket, err := m.store.BeginPreviewAt(id, index)
	if err != nil {
		return err
	}
	task := previewTask{ticket: ticket, payload: job.Payload, settings: settings, index: index}

	m.mu.Lock()
	if !m.enabled || m.closed {
		m.mu.Unlock()
		_ = m.store.FinishPreview(ticket, nil, context.Canceled)
		return errPreviewDisabled
	}
	if !m.started {
		m.started = true
		for range previewWorkers {
			m.wg.Add(1)
			go m.worker()
		}
	}
	select {
	case m.queue <- task:
		m.mu.Unlock()
		return nil
	default:
		m.mu.Unlock()
		_ = m.store.FinishPreview(ticket, nil, errPreviewQueueFull)
		return errPreviewQueueFull
	}
}

func (m *previewManager) worker() {
	defer m.wg.Done()
	for {
		select {
		case <-m.ctx.Done():
			return
		case task := <-m.queue:
			m.render(task)
		}
	}
}

func (m *previewManager) render(task previewTask) {
	if err := task.ticket.Context.Err(); err != nil {
		_ = m.store.FinishPreview(task.ticket, nil, err)
		return
	}
	if err := m.store.StartPreview(task.ticket); err != nil {
		_ = m.store.FinishPreview(task.ticket, nil, err)
		return
	}
	ctx, cancel := context.WithTimeout(task.ticket.Context, previewRequestTimeout)
	defer cancel()
	image, err := m.renderer.Render(ctx, task.payload, task.settings, task.index)
	if err == nil {
		err = validatePreviewPNG(image)
	}
	_ = m.store.FinishPreview(task.ticket, image, err)
}

// Close cancels workers and prevents new external requests.
func (m *previewManager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	m.enabled = false
	m.cancel()
	started := m.started
	m.mu.Unlock()
	m.store.DisablePendingPreviews()
	if started {
		m.wg.Wait()
	}
	for {
		select {
		case task := <-m.queue:
			_ = m.store.FinishPreview(task.ticket, nil, context.Canceled)
		default:
			return
		}
	}
}

// detectZPL is a conservative hint based on format delimiters, not a parser.
func detectZPL(payload []byte) string {
	text := strings.ToUpper(string(payload))
	if strings.Contains(text, "^XA") && strings.Contains(text, "^XZ") {
		return "zpl"
	}
	return "raw"
}

type labelaryRenderer struct {
	client *http.Client
}

func (r labelaryRenderer) Render(ctx context.Context, payload []byte, settings config.PreviewConfig, index int) ([]byte, error) {
	if index < 0 || index > maxLabelIndex {
		return nil, fmt.Errorf("label index must be between 0 and %d", maxLabelIndex)
	}
	density := strconv.Itoa(settings.PrintDensityDpmm) + "dpmm"
	width := strconv.FormatFloat(settings.LabelWidthInches, 'f', -1, 64)
	height := strconv.FormatFloat(settings.LabelHeightInches, 'f', -1, 64)
	endpoint := "https://api.labelary.com/v1/printers/" + density + "/labels/" +
		url.PathEscape(width) + "x" + url.PathEscape(height) + "/" + strconv.Itoa(index)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create Labelary request: %w", err)
	}
	req.Header.Set("Accept", "image/png")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := r.client
	if client == nil {
		client = &http.Client{Timeout: previewRequestTimeout}
	} else {
		clientCopy := *client
		client = &clientCopy
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("labelary request failed: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("labelary returned HTTP %d", response.StatusCode)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "image/png" {
		return nil, errors.New("labelary returned a non-PNG response")
	}
	if response.ContentLength > maxPreviewResponse {
		return nil, capture.ErrPreviewTooLarge
	}
	result, err := io.ReadAll(io.LimitReader(response.Body, maxPreviewResponse+1))
	if err != nil {
		return nil, fmt.Errorf("read Labelary response: %w", err)
	}
	if len(result) > maxPreviewResponse {
		return nil, capture.ErrPreviewTooLarge
	}
	return result, nil
}

func validatePreviewPNG(data []byte) error {
	if len(data) == 0 || len(data) > maxPreviewResponse {
		return capture.ErrPreviewTooLarge
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("invalid PNG preview: %w", err)
	}
	if config.Width < 1 || config.Height < 1 || config.Width > maxPreviewImageSide ||
		config.Height > maxPreviewImageSide || config.Width*config.Height > maxPreviewImagePixels {
		return errors.New("PNG preview dimensions exceed limit")
	}
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		return fmt.Errorf("invalid PNG image data: %w", err)
	}
	return nil
}
