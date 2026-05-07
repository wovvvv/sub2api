package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type stubCodexRegistrationWorkflowService struct {
	scanResult []service.CodexRegistrationCandidateState
	scanErr    error
	scanModel  string

	stageErr   error
	stageIDs   []int64
	unstageErr error
	unstageIDs []int64

	importResult *service.CodexRegistrationImportResult
	importErr    error
	importInput  service.CodexRegistrationImportInput
}

func (s *stubCodexRegistrationWorkflowService) Scan(_ context.Context, model string) ([]service.CodexRegistrationCandidateState, error) {
	s.scanModel = model
	if s.scanErr != nil {
		return nil, s.scanErr
	}
	return s.scanResult, nil
}

func (s *stubCodexRegistrationWorkflowService) Stage(_ context.Context, candidateIDs []int64) error {
	s.stageIDs = append([]int64(nil), candidateIDs...)
	return s.stageErr
}

func (s *stubCodexRegistrationWorkflowService) Unstage(_ context.Context, candidateIDs []int64) error {
	s.unstageIDs = append([]int64(nil), candidateIDs...)
	return s.unstageErr
}

func (s *stubCodexRegistrationWorkflowService) Import(_ context.Context, input service.CodexRegistrationImportInput) (*service.CodexRegistrationImportResult, error) {
	s.importInput = input
	if s.importErr != nil {
		return nil, s.importErr
	}
	if s.importResult == nil {
		return &service.CodexRegistrationImportResult{
			ImportedIDs: []int64{},
			Failed:      map[int64]string{},
		}, nil
	}
	return s.importResult, nil
}

type stubCodexRegistrationCandidateRepo struct {
	listResult []service.CodexRegistrationCandidateState
	listErr    error
	clearAll   int
	clearErr   error
}

func (r *stubCodexRegistrationCandidateRepo) List(_ context.Context) ([]service.CodexRegistrationCandidateState, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.listResult, nil
}

func (r *stubCodexRegistrationCandidateRepo) GetByID(_ context.Context, _ int64) (*service.CodexRegistrationCandidateState, error) {
	return nil, errors.New("not implemented")
}

func (r *stubCodexRegistrationCandidateRepo) ListByIDs(_ context.Context, _ []int64) ([]service.CodexRegistrationCandidateState, error) {
	return nil, errors.New("not implemented")
}

func (r *stubCodexRegistrationCandidateRepo) UpsertBySourcePath(_ context.Context, _ service.CodexRegistrationCandidateState) (*service.CodexRegistrationCandidateState, error) {
	return nil, errors.New("not implemented")
}

func (r *stubCodexRegistrationCandidateRepo) Update(_ context.Context, _ service.CodexRegistrationCandidateState, _ *service.CodexRegistrationCandidateState) (*service.CodexRegistrationCandidateState, error) {
	return nil, errors.New("not implemented")
}

func (r *stubCodexRegistrationCandidateRepo) UpdateBatch(_ context.Context, _ []service.CodexRegistrationCandidateState, _ map[int64]service.CodexRegistrationCandidateState) error {
	return errors.New("not implemented")
}

func (r *stubCodexRegistrationCandidateRepo) DeleteNonImportedBySourcePathsNotIn(_ context.Context, _ []string, _ time.Time) error {
	return errors.New("not implemented")
}

func (r *stubCodexRegistrationCandidateRepo) DeleteAll(_ context.Context) (int, error) {
	if r.clearErr != nil {
		return 0, r.clearErr
	}
	return r.clearAll, nil
}

func setupCodexRegistrationHandlerRouter(
	workflowSvc *stubCodexRegistrationWorkflowService,
	repo *stubCodexRegistrationCandidateRepo,
) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewCodexRegistrationHandler(workflowSvc, repo)

	base := router.Group("/api/v1/admin/account-registration/codex")
	{
		base.POST("/scan", handler.Scan)
		base.GET("/scan/:taskID", handler.GetScanTask)
		base.GET("/candidates", handler.ListCandidates)
		base.DELETE("/candidates", handler.ClearCandidates)
		base.POST("/candidates/select", handler.SelectCandidates)
		base.POST("/candidates/unselect", handler.UnselectCandidates)
		base.POST("/import", handler.ImportCandidates)
	}

	return router
}

func TestCodexRegistrationHandlerScan(t *testing.T) {
	now := time.Date(2026, 4, 10, 11, 12, 13, 0, time.UTC)
	workflowSvc := &stubCodexRegistrationWorkflowService{
		scanResult: []service.CodexRegistrationCandidateState{
			{
				ID:             1,
				SourceFilename: "alive.json",
				SourcePath:     "/host-cli-proxy-api/alive.json",
				Email:          "alive@example.com",
				AccountID:      "acc-1",
				LivenessStatus: service.CodexRegistrationLivenessAlive,
				WorkflowState:  service.CodexRegistrationWorkflowDetected,
				LastCheckedAt:  now,
				UpdatedAt:      now,
			},
		},
	}
	router := setupCodexRegistrationHandlerRouter(workflowSvc, &stubCodexRegistrationCandidateRepo{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/account-registration/codex/scan", bytes.NewBufferString(`{"model":"gpt-5.4-mini"}`))
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Code int `json:"code"`
		Data struct {
			TaskID string `json:"task_id"`
			Status string `json:"status"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.NotEmpty(t, resp.Data.TaskID)
	require.Contains(t, []string{"queued", "running", "succeeded"}, resp.Data.Status)

	require.Eventually(t, func() bool {
		statusRec := httptest.NewRecorder()
		statusReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/account-registration/codex/scan/"+resp.Data.TaskID, nil)
		router.ServeHTTP(statusRec, statusReq)
		if statusRec.Code != http.StatusOK {
			return false
		}

		require.NotContains(t, statusRec.Body.String(), "refresh_token")
		require.NotContains(t, statusRec.Body.String(), "access_token")
		require.NotContains(t, statusRec.Body.String(), "id_token")

		var statusResp struct {
			Code int `json:"code"`
			Data struct {
				TaskID  string `json:"task_id"`
				Status  string `json:"status"`
				Scanned int    `json:"scanned"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(statusRec.Body.Bytes(), &statusResp))
		require.Equal(t, 0, statusResp.Code)
		require.Equal(t, resp.Data.TaskID, statusResp.Data.TaskID)
		if statusResp.Data.Status != "succeeded" {
			return false
		}
		require.Equal(t, 1, statusResp.Data.Scanned)
		return true
	}, time.Second, 10*time.Millisecond)

	require.Equal(t, "gpt-5.4-mini", workflowSvc.scanModel)
}

func TestCodexRegistrationHandlerListCandidates(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	router := setupCodexRegistrationHandlerRouter(
		&stubCodexRegistrationWorkflowService{},
		&stubCodexRegistrationCandidateRepo{
			listResult: []service.CodexRegistrationCandidateState{
				{
					ID:             1,
					SourceFilename: "alive.json",
					SourcePath:     "/host-cli-proxy-api/alive.json",
					Email:          "alice@example.com",
					AccountID:      "acc-alice",
					LivenessStatus: service.CodexRegistrationLivenessAlive,
					WorkflowState:  service.CodexRegistrationWorkflowDetected,
					LastCheckedAt:  now,
					UpdatedAt:      now,
				},
				{
					ID:             2,
					SourceFilename: "staged.json",
					SourcePath:     "/host-cli-proxy-api/staged.json",
					Email:          "alice@example.com",
					AccountID:      "acc-staged",
					LivenessStatus: service.CodexRegistrationLivenessAlive,
					WorkflowState:  service.CodexRegistrationWorkflowStaged,
					LastCheckedAt:  now,
					UpdatedAt:      now,
				},
				{
					ID:             3,
					SourceFilename: "dead.json",
					SourcePath:     "/host-cli-proxy-api/dead.json",
					Email:          "bob@example.com",
					AccountID:      "acc-bob",
					LivenessStatus: service.CodexRegistrationLivenessDead,
					WorkflowState:  service.CodexRegistrationWorkflowDetected,
					LastCheckedAt:  now,
					UpdatedAt:      now,
				},
			},
		},
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/account-registration/codex/candidates?workflow_state=staged&liveness_status=alive&q=alice", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Code int `json:"code"`
		Data struct {
			Total int `json:"total"`
			Items []struct {
				ID            int64  `json:"id"`
				WorkflowState string `json:"workflow_state"`
				LivenessState string `json:"liveness_status"`
			} `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Equal(t, 1, resp.Data.Total)
	require.Len(t, resp.Data.Items, 1)
	require.Equal(t, int64(2), resp.Data.Items[0].ID)
	require.Equal(t, string(service.CodexRegistrationWorkflowStaged), resp.Data.Items[0].WorkflowState)
	require.Equal(t, string(service.CodexRegistrationLivenessAlive), resp.Data.Items[0].LivenessState)
}

func TestCodexRegistrationHandlerListCandidatesInvalidFilterReason(t *testing.T) {
	router := setupCodexRegistrationHandlerRouter(
		&stubCodexRegistrationWorkflowService{},
		&stubCodexRegistrationCandidateRepo{},
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/account-registration/codex/candidates?liveness_status=weird", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	var resp struct {
		Code   int    `json:"code"`
		Reason string `json:"reason"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, http.StatusBadRequest, resp.Code)
	require.Equal(t, "INVALID_LIVENESS_STATUS", resp.Reason)
}

func TestCodexRegistrationHandlerSelectCandidates(t *testing.T) {
	workflowSvc := &stubCodexRegistrationWorkflowService{}
	router := setupCodexRegistrationHandlerRouter(workflowSvc, &stubCodexRegistrationCandidateRepo{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/account-registration/codex/candidates/select", bytes.NewBufferString(`{"candidate_ids":[11,12,11]}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, []int64{11, 12}, workflowSvc.stageIDs)
	var resp struct {
		Code int `json:"code"`
		Data struct {
			Selected int `json:"selected"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 2, resp.Data.Selected)
}

func TestCodexRegistrationHandlerUnselectCandidates(t *testing.T) {
	workflowSvc := &stubCodexRegistrationWorkflowService{}
	router := setupCodexRegistrationHandlerRouter(workflowSvc, &stubCodexRegistrationCandidateRepo{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/account-registration/codex/candidates/unselect", bytes.NewBufferString(`{"candidate_ids":[21,22,21]}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, []int64{21, 22}, workflowSvc.unstageIDs)
	var resp struct {
		Code int `json:"code"`
		Data struct {
			Unselected int `json:"unselected"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 2, resp.Data.Unselected)
}

func TestCodexRegistrationHandlerImportCandidates(t *testing.T) {
	workflowSvc := &stubCodexRegistrationWorkflowService{
		importResult: &service.CodexRegistrationImportResult{
			ImportedIDs: []int64{31},
			Failed: map[int64]string{
				32: "duplicate account already exists",
			},
		},
	}
	router := setupCodexRegistrationHandlerRouter(workflowSvc, &stubCodexRegistrationCandidateRepo{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/account-registration/codex/import", bytes.NewBufferString(`{"candidate_ids":[31,32],"group_ids":[9],"proxy_id":7,"notes":"batch import","concurrency":1,"load_factor":1,"priority":1,"rate_multiplier":1,"import_models":true}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, []int64{31, 32}, workflowSvc.importInput.CandidateIDs)
	require.Equal(t, []int64{9}, workflowSvc.importInput.GroupIDs)
	require.NotNil(t, workflowSvc.importInput.ProxyID)
	require.Equal(t, int64(7), *workflowSvc.importInput.ProxyID)
	require.Equal(t, "batch import", workflowSvc.importInput.Notes)
	require.NotNil(t, workflowSvc.importInput.Concurrency)
	require.Equal(t, 1, *workflowSvc.importInput.Concurrency)
	require.NotNil(t, workflowSvc.importInput.LoadFactor)
	require.Equal(t, 1, *workflowSvc.importInput.LoadFactor)
	require.NotNil(t, workflowSvc.importInput.Priority)
	require.Equal(t, 1, *workflowSvc.importInput.Priority)
	require.NotNil(t, workflowSvc.importInput.RateMultiplier)
	require.Equal(t, 1.0, *workflowSvc.importInput.RateMultiplier)
	require.True(t, workflowSvc.importInput.ImportModels)

	var resp struct {
		Code int `json:"code"`
		Data struct {
			ImportedIDs []int64           `json:"imported_ids"`
			Failed      map[string]string `json:"failed"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Equal(t, []int64{31}, resp.Data.ImportedIDs)
	require.Contains(t, resp.Data.Failed, "32")
}

func TestCodexRegistrationHandlerClearCandidates(t *testing.T) {
	router := setupCodexRegistrationHandlerRouter(
		&stubCodexRegistrationWorkflowService{},
		&stubCodexRegistrationCandidateRepo{clearAll: 3},
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/account-registration/codex/candidates", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Code int `json:"code"`
		Data struct {
			Cleared int `json:"cleared"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Equal(t, 3, resp.Data.Cleared)
}

func TestCodexRegistrationHandlerSelectCandidatesGuardedServerSide(t *testing.T) {
	workflowSvc := &stubCodexRegistrationWorkflowService{
		stageErr: infraerrors.BadRequest("INVALID_STAGE_TRANSITION", "candidate cannot be staged from state=duplicate liveness=alive"),
	}
	router := setupCodexRegistrationHandlerRouter(workflowSvc, &stubCodexRegistrationCandidateRepo{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/account-registration/codex/candidates/select", bytes.NewBufferString(`{"candidate_ids":[1]}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	var resp struct {
		Code    int    `json:"code"`
		Reason  string `json:"reason"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, http.StatusBadRequest, resp.Code)
	require.Equal(t, "INVALID_STAGE_TRANSITION", resp.Reason)
}
