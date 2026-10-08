package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/psmMRFP/WhereToLive/backend/internal/api/generated"
	apihttp "github.com/psmMRFP/WhereToLive/backend/internal/api/transport"
)

// SystemHandler serves process and dependency health operations.
type SystemHandler struct {
	ready     func(context.Context) error
	transport *apihttp.Security
}

func newSystemHandler(ready func(context.Context) error, transport *apihttp.Security) *SystemHandler {
	return &SystemHandler{ready: ready, transport: transport}
}

// GetLiveness reports process liveness.
func (h *SystemHandler) GetLiveness(c *gin.Context) {
	c.JSON(http.StatusOK, generated.HealthStatus{Status: generated.Ok})
}

// GetReadiness reports whether configured dependencies are available.
func (h *SystemHandler) GetReadiness(c *gin.Context) {
	if h.ready != nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), time.Second)
		defer cancel()
		if err := h.ready(ctx); err != nil {
			h.transport.Problem(c, http.StatusServiceUnavailable, "service unavailable")
			return
		}
	}
	c.JSON(http.StatusOK, generated.HealthStatus{Status: generated.Ok})
}
