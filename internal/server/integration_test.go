package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"print-bridge/internal/config"
)

type gatedPreviewRenderer struct {
	started chan previewRenderCall
	release chan struct{}
	image   []byte
}

func (r *gatedPreviewRenderer) Render(ctx context.Context, payload []byte, settings config.PreviewConfig, index int) ([]byte, error) {
	select {
	case r.started <- previewRenderCall{payload: bytes.Clone(payload), settings: settings, index: index}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case <-r.release:
		return bytes.Clone(r.image), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestLiveVirtualCaptureAndPhysicalPrinterIntegration(t *testing.T) {
	printer, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = printer.Close() })
	printerHost, printerPortText, err := net.SplitHostPort(printer.Addr().String())
	require.NoError(t, err)
	printerPort, err := strconv.Atoi(printerPortText)
	require.NoError(t, err)
	printerPayloads := make(chan []byte, 1)
	printerErrors := make(chan error, 1)
	go func() {
		conn, acceptErr := printer.Accept()
		if acceptErr != nil {
			printerErrors <- acceptErr
			return
		}
		defer func() { _ = conn.Close() }()
		payload, readErr := io.ReadAll(conn)
		if readErr != nil {
			printerErrors <- readErr
			return
		}
		printerPayloads <- payload
	}()

	payload := []byte("^XA\r\n^FO10,10^FDVM-9 café\x00^FS^XZ\n")
	body, err := json.Marshal(PrintJobRequest{
		PrinterHostname: printerHost,
		PrinterPort:     printerPort,
		Text:            string(payload),
	})
	require.NoError(t, err)
	renderer := &gatedPreviewRenderer{
		started: make(chan previewRenderCall, 1),
		release: make(chan struct{}),
		image:   validPreviewPNG(t),
	}

	virtualConfig := config.Default()
	virtualConfig.DestinationType = config.DestinationVirtual
	virtualConfig.Preview.LabelaryEnabled = true
	virtualServer, virtualClient, virtualURL := startLiveServer(t, virtualConfig, renderer)
	response, err := virtualClient.Post(virtualURL+"/print", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	var virtualResult map[string]string
	require.NoError(t, json.NewDecoder(response.Body).Decode(&virtualResult))
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Equal(t, "virtual", virtualResult["destination"])
	jobID := virtualResult["jobId"]
	require.NotEmpty(t, jobID)

	select {
	case call := <-renderer.started:
		require.Equal(t, payload, call.payload)
		require.Equal(t, virtualConfig.Preview, call.settings)
		require.Zero(t, call.index)
	case <-time.After(time.Second):
		t.Fatal("virtual submission did not queue the enabled fake preview renderer")
	}

	inspectorOrigin := "http://" + virtualServer.Status().Address
	request, err := http.NewRequest(http.MethodGet, virtualURL+"/api/inspector/jobs", nil)
	require.NoError(t, err)
	request.Header.Set("Origin", inspectorOrigin)
	listResponse, err := virtualClient.Do(request)
	require.NoError(t, err)
	var listing struct {
		Jobs []struct {
			ID string `json:"id"`
		} `json:"jobs"`
	}
	require.NoError(t, json.NewDecoder(listResponse.Body).Decode(&listing))
	require.NoError(t, listResponse.Body.Close())
	require.Equal(t, http.StatusOK, listResponse.StatusCode)
	require.Len(t, listing.Jobs, 1)
	require.Equal(t, jobID, listing.Jobs[0].ID)

	request, err = http.NewRequest(http.MethodGet, virtualURL+"/api/inspector/jobs/"+jobID+"/download", nil)
	require.NoError(t, err)
	request.Header.Set("Origin", inspectorOrigin)
	download, err := virtualClient.Do(request)
	require.NoError(t, err)
	downloadedPayload, err := io.ReadAll(download.Body)
	require.NoError(t, err)
	require.NoError(t, download.Body.Close())
	require.Equal(t, http.StatusOK, download.StatusCode)
	require.Equal(t, payload, downloadedPayload)

	select {
	case got := <-printerPayloads:
		t.Fatalf("Virtual mode sent a captured payload to the fake printer: %q", got)
	case err := <-printerErrors:
		t.Fatalf("fake printer failed before Physical mode: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(renderer.release)
	require.Eventually(t, func() bool {
		job, getErr := virtualServer.CaptureStore().Get(jobID)
		return getErr == nil && job.PreviewState == "ready"
	}, time.Second, 5*time.Millisecond)
	request, err = http.NewRequest(http.MethodGet, virtualURL+"/api/inspector/jobs/"+jobID+"/preview", nil)
	require.NoError(t, err)
	request.Header.Set("Origin", inspectorOrigin)
	previewResponse, err := virtualClient.Do(request)
	require.NoError(t, err)
	previewBytes, err := io.ReadAll(previewResponse.Body)
	require.NoError(t, err)
	require.NoError(t, previewResponse.Body.Close())
	require.Equal(t, http.StatusOK, previewResponse.StatusCode)
	require.Equal(t, renderer.image, previewBytes)

	physicalConfig := config.Default()
	_, physicalClient, physicalURL := startLiveServer(t, physicalConfig, nil)
	physicalResponse, err := physicalClient.Post(physicalURL+"/print", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	physicalResponseBody, err := io.ReadAll(physicalResponse.Body)
	require.NoError(t, err)
	require.NoError(t, physicalResponse.Body.Close())
	require.Equal(t, http.StatusOK, physicalResponse.StatusCode, string(physicalResponseBody))

	select {
	case got := <-printerPayloads:
		require.Equal(t, payload, got)
	case err := <-printerErrors:
		t.Fatalf("fake printer failed in Physical mode: %v", err)
	case <-time.After(time.Second):
		t.Fatal("Physical mode did not deliver the payload to the local fake printer")
	}
}

func startLiveServer(t *testing.T, cfg config.Config, renderer previewRenderer) (*Server, *http.Client, string) {
	t.Helper()
	cfg.HTTPPort = freePort(t)
	srv := New(cfg, nil)
	if renderer != nil {
		srv.previews.Close()
		srv.previews = newPreviewManager(srv.captures, renderer)
	}
	require.NoError(t, srv.Start())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, srv.Stop(ctx))
		srv.previews.Close()
		srv.captures.Close()
	})
	return srv, &http.Client{Timeout: 2 * time.Second}, "http://" + srv.Status().Address
}
