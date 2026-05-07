package admin

import (
	"context"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type openAIOAuthDetectionService interface {
	ProbeAccounts(ctx context.Context, accountIDs []int64, model string) (*service.OpenAIOAuthDetectionBatchResult, error)
}

type openAIOAuthDetectionProbeRequest struct {
	AccountIDs []int64 `json:"account_ids"`
	Model      string  `json:"model"`
}

type OpenAIOAuthDetectionHandler struct {
	service openAIOAuthDetectionService
}

func NewOpenAIOAuthDetectionHandler(service openAIOAuthDetectionService) *OpenAIOAuthDetectionHandler {
	return &OpenAIOAuthDetectionHandler{service: service}
}

// ProbeAccounts executes active detection for selected OpenAI OAuth accounts.
// POST /api/v1/admin/account-registration/openai-oauth-detection/probe
func (h *OpenAIOAuthDetectionHandler) ProbeAccounts(c *gin.Context) {
	if h.service == nil {
		response.InternalError(c, "openai oauth detection service is not configured")
		return
	}

	var req openAIOAuthDetectionProbeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	result, err := h.service.ProbeAccounts(c.Request.Context(), req.AccountIDs, req.Model)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	failed := map[string]string{}
	if result != nil {
		for id, message := range result.Failed {
			failed[strconv.FormatInt(id, 10)] = message
		}
		response.Success(c, gin.H{
			"checked":      result.Checked,
			"healthy":      result.Healthy,
			"unauthorized": result.Unauthorized,
			"failed":       failed,
		})
		return
	}

	response.Success(c, gin.H{
		"checked":      0,
		"healthy":      0,
		"unauthorized": 0,
		"failed":       failed,
	})
}
