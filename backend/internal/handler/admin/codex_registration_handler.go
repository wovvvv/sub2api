package admin

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type codexRegistrationWorkflowService interface {
	Scan(ctx context.Context, model string) ([]service.CodexRegistrationCandidateState, error)
	Stage(ctx context.Context, candidateIDs []int64) error
	Unstage(ctx context.Context, candidateIDs []int64) error
	Import(ctx context.Context, input service.CodexRegistrationImportInput) (*service.CodexRegistrationImportResult, error)
}

type codexRegistrationFilter struct {
	LivenessStatus *service.CodexRegistrationLivenessStatus
	WorkflowState  *service.CodexRegistrationWorkflowState
	Query          string
}

type codexRegistrationSelectionRequest struct {
	CandidateIDs []int64 `json:"candidate_ids"`
}

type codexRegistrationScanRequest struct {
	Model string `json:"model"`
}

type codexRegistrationImportRequest struct {
	CandidateIDs   []int64  `json:"candidate_ids"`
	GroupIDs       []int64  `json:"group_ids"`
	ProxyID        *int64   `json:"proxy_id"`
	Notes          string   `json:"notes"`
	Concurrency    *int     `json:"concurrency"`
	LoadFactor     *int     `json:"load_factor"`
	Priority       *int     `json:"priority"`
	RateMultiplier *float64 `json:"rate_multiplier"`
	ImportModels   bool     `json:"import_models"`
}

type codexRegistrationCandidateResponse struct {
	ID                int64   `json:"id"`
	SourcePath        string  `json:"source_path"`
	SourceFilename    string  `json:"source_filename"`
	SourceMtime       string  `json:"source_mtime,omitempty"`
	SourceFingerprint string  `json:"source_fingerprint"`
	Email             string  `json:"email"`
	AccountID         string  `json:"account_id"`
	Type              string  `json:"type"`
	ExpiresAt         *string `json:"expires_at,omitempty"`
	LastRefreshAt     *string `json:"last_refresh_at,omitempty"`
	LivenessStatus    string  `json:"liveness_status"`
	WorkflowState     string  `json:"workflow_state"`
	StatusReason      string  `json:"status_reason"`
	LastCheckedAt     string  `json:"last_checked_at,omitempty"`
	ImportedAccountID *int64  `json:"imported_account_id,omitempty"`
	ImportedAt        *string `json:"imported_at,omitempty"`
	CreatedAt         string  `json:"created_at,omitempty"`
	UpdatedAt         string  `json:"updated_at,omitempty"`
	CanStage          bool    `json:"can_stage"`
	CanUnstage        bool    `json:"can_unstage"`
	CanImport         bool    `json:"can_import"`
}

// CodexRegistrationHandler handles admin codex account registration workflow endpoints.
type CodexRegistrationHandler struct {
	workflow codexRegistrationWorkflowService
	repo     service.CodexRegistrationCandidateRepository
}

func NewCodexRegistrationHandler(
	workflow codexRegistrationWorkflowService,
	repo service.CodexRegistrationCandidateRepository,
) *CodexRegistrationHandler {
	return &CodexRegistrationHandler{
		workflow: workflow,
		repo:     repo,
	}
}

// Scan triggers source scan and refreshes candidate states.
// POST /api/v1/admin/account-registration/codex/scan
func (h *CodexRegistrationHandler) Scan(c *gin.Context) {
	if h.workflow == nil {
		response.InternalError(c, "codex registration workflow is not configured")
		return
	}

	var req codexRegistrationScanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	candidates, err := h.workflow.Scan(c.Request.Context(), strings.TrimSpace(req.Model))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	items := make([]codexRegistrationCandidateResponse, 0, len(candidates))
	for i := range candidates {
		items = append(items, codexRegistrationCandidateToResponse(candidates[i]))
	}

	response.Success(c, gin.H{
		"scanned":    len(items),
		"candidates": items,
	})
}

// ListCandidates returns persisted candidates with optional filters.
// GET /api/v1/admin/account-registration/codex/candidates
func (h *CodexRegistrationHandler) ListCandidates(c *gin.Context) {
	if h.repo == nil {
		response.InternalError(c, "codex registration candidate repository is not configured")
		return
	}

	filter, err := parseCodexRegistrationFilter(c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	candidates, err := h.repo.List(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	filtered := make([]service.CodexRegistrationCandidateState, 0, len(candidates))
	for i := range candidates {
		if !matchesCodexRegistrationFilter(candidates[i], filter) {
			continue
		}
		filtered = append(filtered, candidates[i])
	}

	items := make([]codexRegistrationCandidateResponse, 0, len(filtered))
	for i := range filtered {
		items = append(items, codexRegistrationCandidateToResponse(filtered[i]))
	}

	response.Success(c, gin.H{
		"total": len(items),
		"items": items,
	})
}

// ClearCandidates removes all persisted codex registration candidates.
// DELETE /api/v1/admin/account-registration/codex/candidates
func (h *CodexRegistrationHandler) ClearCandidates(c *gin.Context) {
	if h.repo == nil {
		response.InternalError(c, "codex registration candidate repository is not configured")
		return
	}

	cleared, err := h.repo.DeleteAll(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, gin.H{"cleared": cleared})
}

// SelectCandidates stages candidates for import.
// POST /api/v1/admin/account-registration/codex/candidates/select
func (h *CodexRegistrationHandler) SelectCandidates(c *gin.Context) {
	if h.workflow == nil {
		response.InternalError(c, "codex registration workflow is not configured")
		return
	}

	var req codexRegistrationSelectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	ids := uniqueCodexRegistrationIDs(req.CandidateIDs)

	if err := h.workflow.Stage(c.Request.Context(), ids); err != nil {
		writeCodexRegistrationWorkflowError(c, err)
		return
	}

	response.Success(c, gin.H{"selected": len(ids)})
}

// UnselectCandidates removes candidates from staged set.
// POST /api/v1/admin/account-registration/codex/candidates/unselect
func (h *CodexRegistrationHandler) UnselectCandidates(c *gin.Context) {
	if h.workflow == nil {
		response.InternalError(c, "codex registration workflow is not configured")
		return
	}

	var req codexRegistrationSelectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	ids := uniqueCodexRegistrationIDs(req.CandidateIDs)

	if err := h.workflow.Unstage(c.Request.Context(), ids); err != nil {
		writeCodexRegistrationWorkflowError(c, err)
		return
	}

	response.Success(c, gin.H{"unselected": len(ids)})
}

// ImportCandidates imports selected staged candidates.
// POST /api/v1/admin/account-registration/codex/import
func (h *CodexRegistrationHandler) ImportCandidates(c *gin.Context) {
	if h.workflow == nil {
		response.InternalError(c, "codex registration workflow is not configured")
		return
	}

	var req codexRegistrationImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	result, err := h.workflow.Import(c.Request.Context(), service.CodexRegistrationImportInput{
		CandidateIDs:   req.CandidateIDs,
		GroupIDs:       req.GroupIDs,
		ProxyID:        req.ProxyID,
		Notes:          req.Notes,
		Concurrency:    req.Concurrency,
		LoadFactor:     req.LoadFactor,
		Priority:       req.Priority,
		RateMultiplier: req.RateMultiplier,
		ImportModels:   req.ImportModels,
	})
	if err != nil {
		writeCodexRegistrationWorkflowError(c, err)
		return
	}

	failed := map[string]string{}
	if result != nil {
		for id, msg := range result.Failed {
			failed[strconv.FormatInt(id, 10)] = msg
		}
		response.Success(c, gin.H{
			"imported_ids": result.ImportedIDs,
			"failed":       failed,
		})
		return
	}

	response.Success(c, gin.H{
		"imported_ids": []int64{},
		"failed":       failed,
	})
}

func parseCodexRegistrationFilter(c *gin.Context) (codexRegistrationFilter, error) {
	filter := codexRegistrationFilter{
		Query: strings.TrimSpace(c.Query("q")),
	}

	if raw := strings.TrimSpace(c.Query("liveness_status")); raw != "" {
		status := service.CodexRegistrationLivenessStatus(strings.ToLower(raw))
		if !slices.Contains(service.CodexRegistrationLivenessStatuses(), status) {
			return filter, infraerrors.BadRequest("INVALID_LIVENESS_STATUS", "invalid liveness_status filter")
		}
		filter.LivenessStatus = &status
	}

	if raw := strings.TrimSpace(c.Query("workflow_state")); raw != "" {
		state := service.CodexRegistrationWorkflowState(strings.ToLower(raw))
		if !slices.Contains(service.CodexRegistrationWorkflowStates(), state) {
			return filter, infraerrors.BadRequest("INVALID_WORKFLOW_STATE", "invalid workflow_state filter")
		}
		filter.WorkflowState = &state
	}

	return filter, nil
}

func matchesCodexRegistrationFilter(candidate service.CodexRegistrationCandidateState, filter codexRegistrationFilter) bool {
	if filter.LivenessStatus != nil && candidate.LivenessStatus != *filter.LivenessStatus {
		return false
	}
	if filter.WorkflowState != nil && candidate.WorkflowState != *filter.WorkflowState {
		return false
	}

	if filter.Query == "" {
		return true
	}
	q := strings.ToLower(filter.Query)
	return strings.Contains(strings.ToLower(candidate.Email), q) ||
		strings.Contains(strings.ToLower(candidate.AccountID), q) ||
		strings.Contains(strings.ToLower(candidate.SourceFilename), q) ||
		strings.Contains(strings.ToLower(candidate.SourcePath), q)
}

func codexRegistrationCandidateToResponse(candidate service.CodexRegistrationCandidateState) codexRegistrationCandidateResponse {
	return codexRegistrationCandidateResponse{
		ID:                candidate.ID,
		SourcePath:        candidate.SourcePath,
		SourceFilename:    candidate.SourceFilename,
		SourceMtime:       formatCodexRegistrationTime(candidate.SourceMtime),
		SourceFingerprint: candidate.SourceFingerprint,
		Email:             candidate.Email,
		AccountID:         candidate.AccountID,
		Type:              candidate.Type,
		ExpiresAt:         formatCodexRegistrationTimePtr(candidate.ExpiresAt),
		LastRefreshAt:     formatCodexRegistrationTimePtr(candidate.LastRefreshAt),
		LivenessStatus:    string(candidate.LivenessStatus),
		WorkflowState:     string(candidate.WorkflowState),
		StatusReason:      candidate.StatusReason,
		LastCheckedAt:     formatCodexRegistrationTime(candidate.LastCheckedAt),
		ImportedAccountID: candidate.ImportedAccountID,
		ImportedAt:        formatCodexRegistrationTimePtr(candidate.ImportedAt),
		CreatedAt:         formatCodexRegistrationTime(candidate.CreatedAt),
		UpdatedAt:         formatCodexRegistrationTime(candidate.UpdatedAt),
		CanStage:          candidate.CanStage(),
		CanUnstage:        candidate.CanUnstage(),
		CanImport:         candidate.WorkflowState == service.CodexRegistrationWorkflowStaged && candidate.LivenessStatus == service.CodexRegistrationLivenessAlive,
	}
}

func formatCodexRegistrationTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func formatCodexRegistrationTimePtr(t *time.Time) *string {
	if t == nil || t.IsZero() {
		return nil
	}
	formatted := t.UTC().Format(time.RFC3339)
	return &formatted
}

func writeCodexRegistrationWorkflowError(c *gin.Context, err error) {
	if err == nil {
		return
	}
	if infraerrors.IsBadRequest(err) {
		response.ErrorFrom(c, err)
		return
	}
	response.ErrorFrom(c, err)
}

func uniqueCodexRegistrationIDs(ids []int64) []int64 {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
