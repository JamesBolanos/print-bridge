package server

import (
	"embed"
	"errors"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"print-bridge/internal/capture"
)

const inspectorPrefix = "/api/inspector"

//go:embed inspector
var inspectorAssets embed.FS

type inspectorJob struct {
	ID                string `json:"id"`
	ReceivedAt        string `json:"receivedAt"`
	Target            string `json:"target"`
	ByteCount         int    `json:"byteCount"`
	DetectedType      string `json:"detectedType"`
	CaptureState      string `json:"captureState"`
	PreviewState      string `json:"previewState"`
	PreviewError      string `json:"previewError,omitempty"`
	PreviewLabelIndex int    `json:"previewLabelIndex"`
	DownloadURL       string `json:"downloadUrl,omitempty"`
	PreviewURL        string `json:"previewUrl,omitempty"`
}

// isInspectorRequest identifies paths that use the inspector's local-origin
// policy instead of the integration API's CORS allow-list.
func isInspectorRequest(path string) bool {
	return path == "/inspector" || strings.HasPrefix(path, "/inspector/") ||
		path == inspectorPrefix || strings.HasPrefix(path, inspectorPrefix+"/")
}

func registerInspectorRoutes(router *gin.Engine, opts Options) {
	router.GET("/inspector", inspectorAccess(opts.Config.HTTPPort, false), func(c *gin.Context) {
		c.Redirect(http.StatusTemporaryRedirect, "/inspector/")
	})
	router.GET("/inspector/", inspectorAccess(opts.Config.HTTPPort, false), func(c *gin.Context) {
		page, err := inspectorAssets.ReadFile("inspector/index.html")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": "inspector assets unavailable"})
			return
		}
		setInspectorContentSecurityPolicy(c)
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.Data(http.StatusOK, "text/html; charset=utf-8", page)
	})
	router.GET("/inspector/app.js", inspectorAccess(opts.Config.HTTPPort, false), func(c *gin.Context) {
		serveInspectorAsset(c, "inspector/app.js", "text/javascript; charset=utf-8")
	})
	router.GET("/inspector/styles.css", inspectorAccess(opts.Config.HTTPPort, false), func(c *gin.Context) {
		serveInspectorAsset(c, "inspector/styles.css", "text/css; charset=utf-8")
	})

	api := router.Group(inspectorPrefix, inspectorAccess(opts.Config.HTTPPort, false))
	api.GET("/jobs", func(c *gin.Context) {
		jobs := opts.CaptureStore.List()
		response := make([]inspectorJob, 0, len(jobs))
		for _, job := range jobs {
			response = append(response, toInspectorJob(job, false))
		}
		c.JSON(http.StatusOK, gin.H{"jobs": response, "previewEnabled": opts.previews.Enabled()})
	})
	api.GET("/jobs/:id", func(c *gin.Context) {
		job, err := opts.CaptureStore.Get(c.Param("id"))
		if errors.Is(err, capture.ErrNotFound) {
			inspectorError(c, http.StatusNotFound, "job not found")
			return
		}
		if err != nil {
			inspectorError(c, http.StatusServiceUnavailable, "capture store unavailable")
			return
		}
		c.JSON(http.StatusOK, gin.H{"job": toInspectorJob(job.Metadata, true)})
	})
	api.GET("/jobs/:id/download", func(c *gin.Context) {
		job, err := opts.CaptureStore.Get(c.Param("id"))
		if errors.Is(err, capture.ErrNotFound) {
			inspectorError(c, http.StatusNotFound, "job not found")
			return
		}
		if err != nil {
			inspectorError(c, http.StatusServiceUnavailable, "capture store unavailable")
			return
		}
		filename := mime.FormatMediaType("attachment", map[string]string{
			"filename": "print-job-" + c.Param("id") + ".bin",
		})
		c.Header("Content-Disposition", filename)
		c.Data(http.StatusOK, "application/octet-stream", job.Payload)
	})
	api.GET("/jobs/:id/preview", func(c *gin.Context) {
		job, err := opts.CaptureStore.Get(c.Param("id"))
		if errors.Is(err, capture.ErrNotFound) {
			inspectorError(c, http.StatusNotFound, "job not found")
			return
		}
		if err != nil {
			inspectorError(c, http.StatusServiceUnavailable, "capture store unavailable")
			return
		}
		if job.PreviewState != "ready" || len(job.Preview) == 0 {
			c.JSON(http.StatusConflict, gin.H{
				"message":      "preview not available",
				"previewState": job.PreviewState,
			})
			return
		}
		c.Data(http.StatusOK, "image/png", job.Preview)
	})
	api.POST("/jobs/:id/preview", inspectorAccess(opts.Config.HTTPPort, true), func(c *gin.Context) {
		var request struct {
			LabelIndex int `json:"labelIndex"`
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
		if err := c.ShouldBindJSON(&request); err != nil {
			inspectorError(c, http.StatusBadRequest, "labelIndex must be a zero-based integer")
			return
		}
		err := opts.previews.Schedule(c.Param("id"), request.LabelIndex)
		switch {
		case err == nil:
			c.JSON(http.StatusAccepted, gin.H{"message": "preview queued"})
		case errors.Is(err, capture.ErrNotFound):
			inspectorError(c, http.StatusNotFound, "job not found")
		case errors.Is(err, errPreviewDisabled):
			inspectorError(c, http.StatusConflict, "preview rendering is disabled")
		case errors.Is(err, errPreviewUnsupported):
			inspectorError(c, http.StatusUnprocessableEntity, "job is not recognized as ZPL")
		case errors.Is(err, capture.ErrPreviewBusy):
			inspectorError(c, http.StatusConflict, "preview is already pending")
		case errors.Is(err, capture.ErrPreviewLimit), errors.Is(err, errPreviewQueueFull):
			inspectorError(c, http.StatusServiceUnavailable, "preview capacity is temporarily unavailable")
		default:
			inspectorError(c, http.StatusBadRequest, err.Error())
		}
	})
	api.DELETE("/jobs", inspectorAccess(opts.Config.HTTPPort, true), func(c *gin.Context) {
		if opts.CaptureStore.Stats().Closed {
			inspectorError(c, http.StatusServiceUnavailable, "capture store unavailable")
			return
		}
		opts.CaptureStore.Clear()
		c.JSON(http.StatusOK, gin.H{"message": "OK"})
	})
}

// inspectorAccess enforces the loopback dashboard policy independently of the
// integration CORS allow-list; mutations require an exact browser Origin.
func inspectorAccess(port int, mutation bool) gin.HandlerFunc {
	expectedHost := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	expectedOrigin := "http://" + expectedHost

	return func(c *gin.Context) {
		setInspectorSecurityHeaders(c)
		if c.Request.Host != expectedHost {
			inspectorError(c, http.StatusForbidden, "unexpected Host")
			return
		}
		origin := c.Request.Header.Get("Origin")
		fetchSite := strings.ToLower(strings.TrimSpace(c.Request.Header.Get("Sec-Fetch-Site")))
		if origin != "" {
			if origin == "null" || !sameOrigin(origin, expectedOrigin) {
				inspectorError(c, http.StatusForbidden, "foreign Origin")
				return
			}
		}
		if fetchSite != "" && fetchSite != "same-origin" && (mutation || fetchSite != "none") {
			inspectorError(c, http.StatusForbidden, "request is not same-origin")
			return
		}
		if mutation && origin == "" {
			inspectorError(c, http.StatusForbidden, "Origin is required for mutations")
			return
		}
		c.Next()
	}
}

func sameOrigin(raw, expected string) bool {
	origin, err := url.Parse(raw)
	if err != nil || origin.User != nil || origin.Path != "" || origin.RawQuery != "" ||
		origin.Fragment != "" || origin.Opaque != "" {
		return false
	}
	return origin.Scheme == "http" && origin.Host == strings.TrimPrefix(expected, "http://")
}

func setInspectorSecurityHeaders(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("X-Frame-Options", "DENY")
	c.Header("Cross-Origin-Resource-Policy", "same-origin")
	c.Header("Referrer-Policy", "no-referrer")
}

func setInspectorContentSecurityPolicy(c *gin.Context) {
	c.Header("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
}

func serveInspectorAsset(c *gin.Context, name, contentType string) {
	data, err := inspectorAssets.ReadFile(name)
	if err != nil {
		inspectorError(c, http.StatusInternalServerError, "inspector assets unavailable")
		return
	}
	setInspectorContentSecurityPolicy(c)
	c.Data(http.StatusOK, contentType, data)
}

func toInspectorJob(meta capture.Metadata, includeURLs bool) inspectorJob {
	result := inspectorJob{
		ID: meta.ID, ReceivedAt: meta.ReceivedAt.Format("2006-01-02T15:04:05.999999999Z07:00"),
		Target: meta.Target, ByteCount: meta.ByteCount, DetectedType: meta.DetectedType,
		CaptureState: meta.CaptureState, PreviewState: meta.PreviewState, PreviewError: meta.PreviewError,
		PreviewLabelIndex: meta.PreviewLabelIndex,
	}
	if includeURLs {
		base := inspectorPrefix + "/jobs/" + url.PathEscape(meta.ID)
		result.DownloadURL = base + "/download"
		result.PreviewURL = base + "/preview"
	}
	return result
}

func inspectorError(c *gin.Context, status int, message string) {
	c.AbortWithStatusJSON(status, gin.H{"message": message})
}
