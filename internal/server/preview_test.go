package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"print-bridge/internal/capture"
	"print-bridge/internal/config"
)

type previewRenderCall struct {
	payload  []byte
	settings config.PreviewConfig
	index    int
}

type fakePreviewRenderer struct {
	calls chan previewRenderCall
	image []byte
	err   error
}

func (f *fakePreviewRenderer) Render(_ context.Context, payload []byte, settings config.PreviewConfig, index int) ([]byte, error) {
	f.calls <- previewRenderCall{payload: bytes.Clone(payload), settings: settings, index: index}
	return bytes.Clone(f.image), f.err
}

func TestInspectorExplicitPreviewUsesStoredZPLAndSelectedLabelIndex(t *testing.T) {
	store := capture.NewStore()
	t.Cleanup(store.Close)
	cfg := config.Default()
	cfg.DestinationType = config.DestinationVirtual
	cfg.Preview.LabelaryEnabled = true
	renderer := &fakePreviewRenderer{calls: make(chan previewRenderCall, 1), image: validPreviewPNG(t)}
	manager := newPreviewManager(store, renderer)
	t.Cleanup(manager.Close)
	manager.Configure(cfg.Preview, true)

	payload := []byte("^XA^FO20,20^FDlabel^FS^XZ")
	job, err := store.Capture(context.Background(), "", payload)
	require.NoError(t, err)
	require.NoError(t, store.Classify(job.ID, "zpl", "disabled"))
	router := NewRouter(Options{Config: cfg, CaptureStore: store, previews: manager})

	foreign := httptest.NewRequest(http.MethodPost, "/api/inspector/jobs/"+job.ID+"/preview", strings.NewReader(`{"labelIndex":2}`))
	foreign.Host = "127.0.0.1:8080"
	foreign.Header.Set("Origin", "https://evil.example")
	foreign.Header.Set("Sec-Fetch-Site", "cross-site")
	foreign.Header.Set("Content-Type", "application/json")
	foreignResponse := httptest.NewRecorder()
	router.ServeHTTP(foreignResponse, foreign)
	require.Equal(t, http.StatusForbidden, foreignResponse.Code)
	select {
	case <-renderer.calls:
		t.Fatal("foreign-origin render request reached the renderer")
	default:
	}

	request := httptest.NewRequest(http.MethodPost, "/api/inspector/jobs/"+job.ID+"/preview", strings.NewReader(`{"labelIndex":2}`))
	request.Host = "127.0.0.1:8080"
	request.Header.Set("Origin", testInspectorOrigin(cfg.HTTPPort))
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusAccepted, response.Code, response.Body.String())

	select {
	case call := <-renderer.calls:
		require.Equal(t, payload, call.payload)
		require.Equal(t, 2, call.index)
		require.Equal(t, cfg.Preview, call.settings)
	case <-time.After(time.Second):
		t.Fatal("renderer was not called")
	}

	require.Eventually(t, func() bool {
		stored, getErr := store.Get(job.ID)
		return getErr == nil && stored.PreviewState == "ready" && stored.PreviewLabelIndex == 2
	}, time.Second, time.Millisecond*5)
	stored, err := store.Get(job.ID)
	require.NoError(t, err)
	require.Equal(t, payload, stored.Payload)
	require.Equal(t, renderer.image, stored.Preview)
}

func TestPreviewRendererPostsRawPayloadToFixedLabelaryPath(t *testing.T) {
	expectedPNG := validPreviewPNG(t)
	var gotMethod, gotURL, gotContentType string
	var gotBody []byte
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		gotMethod = request.Method
		gotURL = request.URL.String()
		gotContentType = request.Header.Get("Content-Type")
		var err error
		gotBody, err = io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"image/png"}},
			Body: io.NopCloser(bytes.NewReader(expectedPNG)), ContentLength: int64(len(expectedPNG)),
		}, nil
	})}
	payload := []byte("^XA^FDsecret-content^XZ")
	settings := config.PreviewConfig{LabelWidthInches: 4, LabelHeightInches: 6, PrintDensityDpmm: 8}
	result, err := (labelaryRenderer{client: client}).Render(context.Background(), payload, settings, 3)
	require.NoError(t, err)
	require.Equal(t, expectedPNG, result)
	require.Equal(t, http.MethodPost, gotMethod)
	require.Equal(t, "https://api.labelary.com/v1/printers/8dpmm/labels/4x6/3", gotURL)
	require.Equal(t, "application/x-www-form-urlencoded", gotContentType)
	require.Equal(t, payload, gotBody)
	require.NotContains(t, gotURL, "secret-content")
}

func TestPreviewRendererRejectsOversizedAndInvalidPNG(t *testing.T) {
	require.Error(t, validatePreviewPNG([]byte("not a png")))
	var large bytes.Buffer
	require.NoError(t, png.Encode(&large, image.NewRGBA(image.Rect(0, 0, maxPreviewImageSide+1, 1))))
	require.Error(t, validatePreviewPNG(large.Bytes()))

	renderer := labelaryRenderer{client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/html"}},
			Body: io.NopCloser(strings.NewReader("provider error")), ContentLength: -1,
		}, nil
	})}}
	_, err := renderer.Render(context.Background(), []byte("^XA^XZ"), config.DefaultPreview(), 0)
	require.ErrorContains(t, err, "non-PNG")

	renderer = labelaryRenderer{client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"image/png"}},
			Body: io.NopCloser(strings.NewReader("")), ContentLength: maxPreviewResponse + 1,
		}, nil
	})}}
	_, err = renderer.Render(context.Background(), []byte("^XA^XZ"), config.DefaultPreview(), 0)
	require.ErrorIs(t, err, capture.ErrPreviewTooLarge)
}

func TestPreviewManagerDisabledAndProviderErrorsPreserveCapture(t *testing.T) {
	store := capture.NewStore()
	t.Cleanup(store.Close)
	job, err := store.Capture(context.Background(), "", []byte("^XA^XZ"))
	require.NoError(t, err)
	require.NoError(t, store.Classify(job.ID, "zpl", "disabled"))

	renderer := &fakePreviewRenderer{calls: make(chan previewRenderCall, 1), err: errors.New("provider unavailable")}
	manager := newPreviewManager(store, renderer)
	t.Cleanup(manager.Close)
	settings := config.DefaultPreview()
	settings.LabelaryEnabled = true
	manager.Configure(settings, true)
	require.Error(t, manager.Schedule("missing", 0))
	require.NoError(t, manager.Schedule(job.ID, 1))
	select {
	case <-renderer.calls:
	case <-time.After(time.Second):
		t.Fatal("renderer was not called")
	}
	require.Eventually(t, func() bool {
		stored, getErr := store.Get(job.ID)
		return getErr == nil && stored.PreviewState == "failed"
	}, time.Second, time.Millisecond*5)
	stored, err := store.Get(job.ID)
	require.NoError(t, err)
	require.Equal(t, []byte("^XA^XZ"), stored.Payload)

	manager.Configure(settings, false)
	require.ErrorIs(t, manager.Schedule(job.ID, 2), errPreviewDisabled)
}

func TestPreviewSettingsChangeInvalidatesPreviewAndSnapshotsNewSettings(t *testing.T) {
	store := capture.NewStore()
	t.Cleanup(store.Close)
	renderer := &fakePreviewRenderer{calls: make(chan previewRenderCall, 2), image: validPreviewPNG(t)}
	manager := newPreviewManager(store, renderer)
	t.Cleanup(manager.Close)
	settings := config.DefaultPreview()
	settings.LabelaryEnabled = true
	manager.Configure(settings, true)
	job, err := store.Capture(context.Background(), "", []byte("^XA^XZ"))
	require.NoError(t, err)
	require.NoError(t, store.Classify(job.ID, "zpl", "disabled"))
	require.NoError(t, manager.Schedule(job.ID, 0))
	select {
	case call := <-renderer.calls:
		require.Equal(t, settings.LabelWidthInches, call.settings.LabelWidthInches)
	case <-time.After(time.Second):
		t.Fatal("renderer was not called")
	}
	require.Eventually(t, func() bool {
		stored, getErr := store.Get(job.ID)
		return getErr == nil && stored.PreviewState == "ready"
	}, time.Second, time.Millisecond*5)

	settings.LabelWidthInches = 5
	manager.Configure(settings, true)
	stored, err := store.Get(job.ID)
	require.NoError(t, err)
	require.Equal(t, []byte("^XA^XZ"), stored.Payload)
	require.Empty(t, stored.Preview)
	require.Equal(t, "disabled", stored.PreviewState)

	require.NoError(t, manager.Schedule(job.ID, 0))
	select {
	case call := <-renderer.calls:
		require.Equal(t, 5.0, call.settings.LabelWidthInches)
	case <-time.After(time.Second):
		t.Fatal("renderer did not receive updated settings")
	}
}

func TestPreviewManagerBoundsPendingWorkAndCancelsWhenDisabled(t *testing.T) {
	store := capture.NewStore()
	t.Cleanup(store.Close)
	started := make(chan struct{}, previewWorkers)
	renderer := blockingPreviewRenderer{started: started}
	manager := newPreviewManager(store, renderer)
	t.Cleanup(manager.Close)
	settings := config.DefaultPreview()
	settings.LabelaryEnabled = true
	manager.Configure(settings, true)

	ids := make([]string, capture.DefaultLimits().PendingPreviews)
	for i := range ids {
		job, err := store.Capture(context.Background(), "", []byte("^XA^XZ"))
		require.NoError(t, err)
		require.NoError(t, store.Classify(job.ID, "zpl", "disabled"))
		ids[i] = job.ID
		require.NoError(t, manager.Schedule(job.ID, 0))
	}
	for range previewWorkers {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("preview workers did not begin")
		}
	}
	extra, err := store.Capture(context.Background(), "", []byte("^XA^XZ"))
	require.NoError(t, err)
	require.NoError(t, store.Classify(extra.ID, "zpl", "disabled"))
	require.ErrorIs(t, manager.Schedule(extra.ID, 0), capture.ErrPreviewLimit)

	manager.Configure(settings, false)
	require.Eventually(t, func() bool {
		for _, id := range ids {
			job, getErr := store.Get(id)
			if getErr != nil || job.PreviewState != "disabled" {
				return false
			}
		}
		return true
	}, time.Second, time.Millisecond*5)
	job, err := store.Get(extra.ID)
	require.NoError(t, err)
	require.Equal(t, []byte("^XA^XZ"), job.Payload)
}

func TestDetectZPLOnlyProvidesHint(t *testing.T) {
	require.Equal(t, "zpl", detectZPL([]byte("prefix ^xa content ^xz suffix")))
	require.Equal(t, "raw", detectZPL([]byte("^XA without end")))
}

func TestVirtualCaptureSchedulesOnlyWhenEnabledAndForLikelyZPL(t *testing.T) {
	store := capture.NewStore()
	t.Cleanup(store.Close)
	renderer := &fakePreviewRenderer{calls: make(chan previewRenderCall, 2), image: validPreviewPNG(t)}
	manager := newPreviewManager(store, renderer)
	t.Cleanup(manager.Close)
	cfg := config.Default()
	cfg.DestinationType = config.DestinationVirtual
	router := NewRouter(Options{Config: cfg, CaptureStore: store, previews: manager})

	send := func(payload string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/print", strings.NewReader(`{"text":`+mustJSON(t, payload)+`}`))
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	require.Equal(t, http.StatusOK, send("^XA^XZ").Code)
	select {
	case <-renderer.calls:
		t.Fatal("renderer was called while disabled")
	default:
	}

	cfg.Preview.LabelaryEnabled = true
	router = NewRouter(Options{Config: cfg, CaptureStore: store, previews: manager})
	require.Equal(t, http.StatusOK, send("plain text").Code)
	select {
	case <-renderer.calls:
		t.Fatal("renderer was called for raw text")
	default:
	}
	require.Equal(t, http.StatusOK, send("^XA^FDnew^XZ").Code)
	select {
	case call := <-renderer.calls:
		require.Equal(t, []byte("^XA^FDnew^XZ"), call.payload)
	case <-time.After(time.Second):
		t.Fatal("new likely-ZPL capture was not scheduled")
	}
}

func TestPreviewActionRejectsInvalidIndex(t *testing.T) {
	store := capture.NewStore()
	t.Cleanup(store.Close)
	cfg := config.Default()
	cfg.DestinationType = config.DestinationVirtual
	cfg.Preview.LabelaryEnabled = true
	manager := newPreviewManager(store, &fakePreviewRenderer{calls: make(chan previewRenderCall, 1)})
	t.Cleanup(manager.Close)
	manager.Configure(cfg.Preview, true)
	router := NewRouter(Options{Config: cfg, CaptureStore: store, previews: manager})
	request := httptest.NewRequest(http.MethodPost, "/api/inspector/jobs/nope/preview", strings.NewReader(`{"labelIndex":100}`))
	request.Host = "127.0.0.1:8080"
	request.Header.Set("Origin", testInspectorOrigin(cfg.HTTPPort))
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	var body map[string]any
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Contains(t, body["message"], "0 and 99")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type blockingPreviewRenderer struct {
	started chan struct{}
}

func (r blockingPreviewRenderer) Render(ctx context.Context, _ []byte, _ config.PreviewConfig, _ int) ([]byte, error) {
	r.started <- struct{}{}
	<-ctx.Done()
	return nil, ctx.Err()
}

func validPreviewPNG(t *testing.T) []byte {
	t.Helper()
	var data bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.Black)
	require.NoError(t, png.Encode(&data, img))
	return data.Bytes()
}

func mustJSON(t *testing.T, value string) string {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return string(data)
}
