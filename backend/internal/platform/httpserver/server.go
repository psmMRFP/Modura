// Package httpserver composes the public HTTP transport.
package httpserver

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/modura-dev/modura/backend/internal/api/generated"
	"github.com/modura-dev/modura/backend/internal/api/handler"
	"github.com/modura-dev/modura/backend/internal/modules/identity"
	"github.com/modura-dev/modura/backend/internal/platform/config"
)

// Dependencies contains runtime application and health dependencies.
type Dependencies struct {
	PublicIdentity         handler.PublicIdentity
	PublicChallengeSiteKey string
	PublicIdentityFailure  func()
	PlatformPlaces         handler.PlatformPlaces
	Places                 handler.Places
	Identity               handler.Identity
	Authorizer             handler.Authorizer
	Authorization          handler.Authorization
	Organization           handler.Organization
	PlatformAdmin          handler.PlatformAdmin
	PlatformTenant         handler.PlatformTenant
	Provisioning           handler.Provisioning
	Settings               handler.Settings
	PlatformSettings       handler.PlatformSettings
	Audit                  handler.Audit
	PlatformAudit          handler.PlatformAudit
	Ready                  func(context.Context) error
}

// New returns a configured HTTP server without starting it.
func New(cfg config.HTTP, logger *slog.Logger, dependencies ...Dependencies) *http.Server {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	metrics := newMetricsRecorder()
	router.Use(gin.Recovery(), requestCorrelation(), traceContext(), limitRequestBody(cfg.MaxBodyBytes), cors(cfg.AllowedOrigins), requestMetrics(metrics), requestLogger(logger))
	var deps Dependencies
	if len(dependencies) > 0 {
		deps = dependencies[0]
	}
	contractHandler := handler.New(handler.Dependencies{PublicIdentity: deps.PublicIdentity, PublicChallengeSiteKey: deps.PublicChallengeSiteKey, PublicIdentityFailure: deps.PublicIdentityFailure, PlatformPlaces: deps.PlatformPlaces, Places: deps.Places, Identity: deps.Identity, Authorizer: deps.Authorizer, Authorization: deps.Authorization, Organization: deps.Organization, PlatformAdmin: deps.PlatformAdmin, PlatformTenant: deps.PlatformTenant, Provisioning: deps.Provisioning, Settings: deps.Settings, PlatformSettings: deps.PlatformSettings, Audit: deps.Audit, PlatformAudit: deps.PlatformAudit, Ready: deps.Ready}, cfg.CookieSecure, func() (string, error) { return identity.NewOpaqueToken(32) })
	generated.RegisterHandlersWithOptions(router, contractHandler, generated.GinServerOptions{
		BaseURL: "/api",
		ErrorHandler: func(c *gin.Context, _ error, status int) {
			c.Header("Content-Type", "application/problem+json")
			c.JSON(status, generated.Problem{Type: "about:blank", Title: "invalid request", Status: status})
		},
	})
	router.GET("/metrics", gin.WrapH(metrics.handler()))
	return &http.Server{Addr: cfg.Address, Handler: router, ReadTimeout: cfg.ReadTimeout, ReadHeaderTimeout: cfg.ReadHeaderTimeout, WriteTimeout: cfg.WriteTimeout, IdleTimeout: cfg.IdleTimeout, MaxHeaderBytes: cfg.MaxHeaderBytes}
}

// limitRequestBody caps every request body up front and again on read, so
// oversized payloads fail fast with 413 instead of being buffered.
func limitRequestBody(limit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body != nil && limit > 0 {
			if c.Request.ContentLength > limit {
				c.AbortWithStatus(http.StatusRequestEntityTooLarge)
				return
			}
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		}
		c.Next()
	}
}

// cors enforces the least-privilege cross-origin policy. Origins not on the
// allowlist receive no CORS headers, so browsers deny them by default.
func cors(allowedOrigins []string) gin.HandlerFunc {
	allowed := make(map[string]bool, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		allowed[origin] = true
	}
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" && allowed[origin] {
			headers := c.Writer.Header()
			headers.Set("Access-Control-Allow-Origin", origin)
			headers.Add("Vary", "Origin")
			headers.Set("Access-Control-Allow-Credentials", "true")
			if c.Request.Method == http.MethodOptions && c.GetHeader("Access-Control-Request-Method") != "" {
				headers.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE")
				headers.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-CSRF-Token, X-Request-ID")
				headers.Set("Access-Control-Max-Age", "600")
				c.AbortWithStatus(http.StatusNoContent)
				return
			}
		}
		c.Next()
	}
}

// traceContext propagates the W3C trace-context trace id, which keeps request
// traces compatible with OpenTelemetry collectors without importing the SDK.
func traceContext() gin.HandlerFunc {
	return func(c *gin.Context) {
		traceID, spanID := parseOrGenerateTraceparent(c.GetHeader("traceparent"))
		c.Set("trace_id", traceID)
		c.Set("span_id", spanID)
		c.Writer.Header().Set("traceparent", fmt.Sprintf("00-%s-%s-01", traceID, spanID))
		c.Next()
	}
}

func parseOrGenerateTraceparent(header string) (traceID, spanID string) {
	parts := strings.Split(strings.TrimSpace(header), "-")
	if len(parts) == 4 && len(parts[1]) == 32 && len(parts[2]) == 16 && allHex(parts[1]) && allHex(parts[2]) && parts[1] != "00000000000000000000000000000000" && parts[2] != "0000000000000000" {
		return parts[1], randomHex(16)
	}
	return randomHex(32), randomHex(16)
}

func allHex(value string) bool {
	for _, symbol := range value {
		if !strings.ContainsRune("0123456789abcdefABCDEF", symbol) {
			return false
		}
	}
	return true
}

func randomHex(length int) string {
	value := uuid.NewString() + uuid.NewString()
	value = strings.ReplaceAll(value, "-", "")
	return value[:length]
}

func requestMetrics(metrics *metricsRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		metrics.observe(c.Request.Method, route, strconv.Itoa(c.Writer.Status()), time.Since(start).Seconds())
	}
}

func requestCorrelation() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := strings.TrimSpace(c.GetHeader("X-Request-ID"))
		if requestID == "" || len(requestID) > 128 {
			requestID = uuid.NewString()
			c.Request.Header.Set("X-Request-ID", requestID)
		}
		c.Header("X-Request-ID", requestID)
		c.Next()
	}
}

// requestLogger emits one redacted line per request. Query strings,
// credentials, and request bodies are deliberately never logged.
func requestLogger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		traceID, _ := c.Get("trace_id")
		logger.InfoContext(c.Request.Context(), "http request",
			"request_id", c.GetHeader("X-Request-ID"),
			"trace_id", traceID,
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status())
	}
}
