package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type stubOpenAIOAuthDetectionService struct {
	receivedIDs []int64
	receivedModel string
	result      *service.OpenAIOAuthDetectionBatchResult
	err         error
}

func (s *stubOpenAIOAuthDetectionService) ProbeAccounts(_ context.Context, accountIDs []int64, model string) (*service.OpenAIOAuthDetectionBatchResult, error) {
	s.receivedIDs = append([]int64(nil), accountIDs...)
	s.receivedModel = model
	if s.err != nil {
		return nil, s.err
	}
	return s.result, nil
}

func setupOpenAIOAuthDetectionRouter(svc *stubOpenAIOAuthDetectionService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewOpenAIOAuthDetectionHandler(svc)
	base := router.Group("/api/v1/admin/account-registration/openai-oauth-detection")
	base.POST("/probe", handler.ProbeAccounts)
	return router
}

func TestOpenAIOAuthDetectionHandlerProbeAccounts(t *testing.T) {
	svc := &stubOpenAIOAuthDetectionService{
		result: &service.OpenAIOAuthDetectionBatchResult{
			Checked:      2,
			Healthy:      1,
			Unauthorized: 1,
			Failed: map[int64]string{
				12: "probe timeout",
			},
		},
	}
	router := setupOpenAIOAuthDetectionRouter(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/account-registration/openai-oauth-detection/probe", bytes.NewBufferString(`{"account_ids":[11,12,11],"model":"gpt-5.4-mini"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, []int64{11, 12, 11}, svc.receivedIDs)
	require.Equal(t, "gpt-5.4-mini", svc.receivedModel)

	var resp struct {
		Code int `json:"code"`
		Data struct {
			Checked      int               `json:"checked"`
			Healthy      int               `json:"healthy"`
			Unauthorized int               `json:"unauthorized"`
			Failed       map[string]string `json:"failed"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Equal(t, 2, resp.Data.Checked)
	require.Equal(t, 1, resp.Data.Healthy)
	require.Equal(t, 1, resp.Data.Unauthorized)
	require.Equal(t, "probe timeout", resp.Data.Failed["12"])
}
