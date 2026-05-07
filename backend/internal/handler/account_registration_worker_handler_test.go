package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type workerImportServiceStub struct {
	lastAuthHeader string
	lastInput      AccountRegistrationWorkerOpenAIRequest
	result         *AccountRegistrationWorkerOpenAIResponse
	err            error
}

func (s *workerImportServiceStub) ImportOpenAI(ctx context.Context, authHeader string, input AccountRegistrationWorkerOpenAIRequest) (*AccountRegistrationWorkerOpenAIResponse, error) {
	s.lastAuthHeader = authHeader
	s.lastInput = input
	if s.err != nil {
		return nil, s.err
	}
	if s.result != nil {
		return s.result, nil
	}
	return &AccountRegistrationWorkerOpenAIResponse{AccountID: 101}, nil
}

func setupWorkerImportRouter(svc *workerImportServiceStub) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewAccountRegistrationWorkerHandler(svc)
	router.POST("/api/v1/account-registration/worker/openai", handler.ImportOpenAI)
	return router
}

func TestAccountRegistrationWorkerHandler(t *testing.T) {
	t.Run("invalid json returns bad request", func(t *testing.T) {
		router := setupWorkerImportRouter(&workerImportServiceStub{})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/account-registration/worker/openai", bytes.NewBufferString("{"))
		req.Header.Set("Authorization", "Bearer worker-secret")
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("service error is mapped", func(t *testing.T) {
		router := setupWorkerImportRouter(&workerImportServiceStub{
			err: infraerrors.Forbidden("ACCOUNT_REGISTRATION_WORKER_UNAUTHORIZED", "invalid worker token"),
		})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/account-registration/worker/openai", bytes.NewBufferString(`{"email":"worker@example.com","refresh_token":"rt","import_options":{"group_id":9}}`))
		req.Header.Set("Authorization", "Bearer wrong-token")
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("valid request succeeds and forwards auth header", func(t *testing.T) {
		svc := &workerImportServiceStub{
			result: &AccountRegistrationWorkerOpenAIResponse{
				AccountID:      123,
				AccountName:    "codex:worker@example.com",
				ImportedModels: []string{"gpt-5.4"},
			},
		}
		router := setupWorkerImportRouter(svc)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/account-registration/worker/openai", bytes.NewBufferString(`{"email":"worker@example.com","refresh_token":"rt","import_options":{"group_ids":[9,10],"import_models":true,"model_whitelist":["gpt-5.4"]}}`))
		req.Header.Set("Authorization", "Bearer worker-secret")
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "Bearer worker-secret", svc.lastAuthHeader)
		require.Equal(t, []int64{9, 10}, svc.lastInput.ImportOptions.GroupIDs)

		var resp struct {
			Code int `json:"code"`
			Data AccountRegistrationWorkerOpenAIResponse `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		require.Equal(t, 0, resp.Code)
		require.Equal(t, int64(123), resp.Data.AccountID)
		require.Equal(t, []string{"gpt-5.4"}, resp.Data.ImportedModels)
	})
}
