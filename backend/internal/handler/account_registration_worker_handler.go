package handler

import (
	"context"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type AccountRegistrationWorkerImportOptions struct {
	GroupID        *int64   `json:"group_id,omitempty"`
	GroupIDs       []int64  `json:"group_ids,omitempty"`
	ProxyID        *int64   `json:"proxy_id,omitempty"`
	Notes          string   `json:"notes,omitempty"`
	Concurrency    *int     `json:"concurrency,omitempty"`
	LoadFactor     *int     `json:"load_factor,omitempty"`
	Priority       *int     `json:"priority,omitempty"`
	RateMultiplier *float64 `json:"rate_multiplier,omitempty"`
	ImportModels   bool     `json:"import_models,omitempty"`
	ModelWhitelist []string `json:"model_whitelist,omitempty"`
}

type AccountRegistrationWorkerOpenAIRequest struct {
	Email         string                                 `json:"email"`
	RefreshToken  string                                 `json:"refresh_token"`
	AccessToken   string                                 `json:"access_token,omitempty"`
	IDToken       string                                 `json:"id_token,omitempty"`
	AccountID     string                                 `json:"account_id,omitempty"`
	ClientID      string                                 `json:"client_id,omitempty"`
	ExpiresAt     string                                 `json:"expires_at,omitempty"`
	ImportOptions AccountRegistrationWorkerImportOptions `json:"import_options"`
}

type AccountRegistrationWorkerOpenAIResponse struct {
	AccountID      int64             `json:"account_id"`
	AccountName    string            `json:"account_name"`
	ImportedModels []string          `json:"imported_models,omitempty"`
	ModelMapping   map[string]string `json:"model_mapping,omitempty"`
}

type accountRegistrationWorkerOpenAIImporter interface {
	ImportOpenAI(ctx context.Context, authHeader string, input AccountRegistrationWorkerOpenAIRequest) (*AccountRegistrationWorkerOpenAIResponse, error)
}

type accountRegistrationWorkerServiceAdapter struct {
	svc *service.AccountRegistrationWorkerService
}

func (a *accountRegistrationWorkerServiceAdapter) ImportOpenAI(ctx context.Context, authHeader string, input AccountRegistrationWorkerOpenAIRequest) (*AccountRegistrationWorkerOpenAIResponse, error) {
	if a == nil || a.svc == nil {
		return nil, nil
	}
	result, err := a.svc.ImportOpenAIWithAuth(ctx, authHeader, service.AccountRegistrationWorkerOpenAIInput{
		Email:        input.Email,
		RefreshToken: input.RefreshToken,
		AccessToken:  input.AccessToken,
		IDToken:      input.IDToken,
		AccountID:    input.AccountID,
		ClientID:     input.ClientID,
		ExpiresAt:    input.ExpiresAt,
		ImportOptions: service.AccountRegistrationWorkerImportOptions{
			GroupID:        input.ImportOptions.GroupID,
			GroupIDs:       append([]int64(nil), input.ImportOptions.GroupIDs...),
			ProxyID:        input.ImportOptions.ProxyID,
			Notes:          input.ImportOptions.Notes,
			Concurrency:    input.ImportOptions.Concurrency,
			LoadFactor:     input.ImportOptions.LoadFactor,
			Priority:       input.ImportOptions.Priority,
			RateMultiplier: input.ImportOptions.RateMultiplier,
			ImportModels:   input.ImportOptions.ImportModels,
			ModelWhitelist: append([]string(nil), input.ImportOptions.ModelWhitelist...),
		},
	})
	if err != nil || result == nil {
		return nil, err
	}
	return &AccountRegistrationWorkerOpenAIResponse{
		AccountID:      result.AccountID,
		AccountName:    result.AccountName,
		ImportedModels: append([]string(nil), result.ImportedModels...),
		ModelMapping:   result.ModelMapping,
	}, nil
}

type AccountRegistrationWorkerHandler struct {
	importer accountRegistrationWorkerOpenAIImporter
}

func NewAccountRegistrationWorkerHandler(importer accountRegistrationWorkerOpenAIImporter) *AccountRegistrationWorkerHandler {
	return &AccountRegistrationWorkerHandler{importer: importer}
}

func ProvideAccountRegistrationWorkerHandler(svc *service.AccountRegistrationWorkerService) *AccountRegistrationWorkerHandler {
	return NewAccountRegistrationWorkerHandler(&accountRegistrationWorkerServiceAdapter{svc: svc})
}

func (h *AccountRegistrationWorkerHandler) ImportOpenAI(c *gin.Context) {
	if h == nil || h.importer == nil {
		response.InternalError(c, "account registration worker importer is not configured")
		return
	}

	var req AccountRegistrationWorkerOpenAIRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	result, err := h.importer.ImportOpenAI(c.Request.Context(), strings.TrimSpace(c.GetHeader("Authorization")), req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, result)
}
