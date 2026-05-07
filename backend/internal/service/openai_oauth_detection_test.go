package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type detectionHTTPUpstreamStub struct {
	response *http.Response
	err      error
	requests []*http.Request
}

func (s *detectionHTTPUpstreamStub) Do(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return nil, nil
}

func (s *detectionHTTPUpstreamStub) DoWithTLS(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	s.requests = append(s.requests, req)
	if s.err != nil {
		return nil, s.err
	}
	return s.response, nil
}

type detectionAccountRepoStub struct {
	accountsByID map[int64]*Account
	setError     []struct {
		id  int64
		msg string
	}
	clearError []int64
	updateExtra map[int64]map[string]any
}

func (s *detectionAccountRepoStub) Create(ctx context.Context, account *Account) error { return nil }
func (s *detectionAccountRepoStub) GetByID(ctx context.Context, id int64) (*Account, error) {
	if account, ok := s.accountsByID[id]; ok {
		return account, nil
	}
	return nil, nil
}
func (s *detectionAccountRepoStub) GetByIDs(ctx context.Context, ids []int64) ([]*Account, error) {
	result := make([]*Account, 0, len(ids))
	for _, id := range ids {
		if account, ok := s.accountsByID[id]; ok {
			result = append(result, account)
		}
	}
	return result, nil
}
func (s *detectionAccountRepoStub) ExistsByID(ctx context.Context, id int64) (bool, error) { return false, nil }
func (s *detectionAccountRepoStub) GetByCRSAccountID(ctx context.Context, crsAccountID string) (*Account, error) {
	return nil, nil
}
func (s *detectionAccountRepoStub) FindByExtraField(ctx context.Context, key string, value any) ([]Account, error) {
	return nil, nil
}
func (s *detectionAccountRepoStub) ListCRSAccountIDs(ctx context.Context) (map[string]int64, error) {
	return nil, nil
}
func (s *detectionAccountRepoStub) Update(ctx context.Context, account *Account) error { return nil }
func (s *detectionAccountRepoStub) Delete(ctx context.Context, id int64) error         { return nil }
func (s *detectionAccountRepoStub) List(ctx context.Context, params pagination.PaginationParams) ([]Account, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (s *detectionAccountRepoStub) ListWithFilters(ctx context.Context, params pagination.PaginationParams, platform, accountType, status, search string, groupID int64, privacyMode string) ([]Account, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (s *detectionAccountRepoStub) ListByGroup(ctx context.Context, groupID int64) ([]Account, error) {
	return nil, nil
}
func (s *detectionAccountRepoStub) ListActive(ctx context.Context) ([]Account, error) { return nil, nil }
func (s *detectionAccountRepoStub) ListByPlatform(ctx context.Context, platform string) ([]Account, error) {
	return nil, nil
}
func (s *detectionAccountRepoStub) UpdateLastUsed(ctx context.Context, id int64) error { return nil }
func (s *detectionAccountRepoStub) BatchUpdateLastUsed(ctx context.Context, updates map[int64]time.Time) error {
	return nil
}
func (s *detectionAccountRepoStub) SetError(ctx context.Context, id int64, errorMsg string) error {
	s.setError = append(s.setError, struct {
		id  int64
		msg string
	}{id: id, msg: errorMsg})
	return nil
}
func (s *detectionAccountRepoStub) ClearError(ctx context.Context, id int64) error {
	s.clearError = append(s.clearError, id)
	return nil
}
func (s *detectionAccountRepoStub) SetSchedulable(ctx context.Context, id int64, schedulable bool) error {
	return nil
}
func (s *detectionAccountRepoStub) AutoPauseExpiredAccounts(ctx context.Context, now time.Time) (int64, error) {
	return 0, nil
}
func (s *detectionAccountRepoStub) BindGroups(ctx context.Context, accountID int64, groupIDs []int64) error {
	return nil
}
func (s *detectionAccountRepoStub) ListSchedulable(ctx context.Context) ([]Account, error) {
	return nil, nil
}
func (s *detectionAccountRepoStub) ListSchedulableByGroupID(ctx context.Context, groupID int64) ([]Account, error) {
	return nil, nil
}
func (s *detectionAccountRepoStub) ListSchedulableByPlatform(ctx context.Context, platform string) ([]Account, error) {
	return nil, nil
}
func (s *detectionAccountRepoStub) ListSchedulableByGroupIDAndPlatform(ctx context.Context, groupID int64, platform string) ([]Account, error) {
	return nil, nil
}
func (s *detectionAccountRepoStub) ListSchedulableByPlatforms(ctx context.Context, platforms []string) ([]Account, error) {
	return nil, nil
}
func (s *detectionAccountRepoStub) ListSchedulableByGroupIDAndPlatforms(ctx context.Context, groupID int64, platforms []string) ([]Account, error) {
	return nil, nil
}
func (s *detectionAccountRepoStub) ListSchedulableUngroupedByPlatform(ctx context.Context, platform string) ([]Account, error) {
	return nil, nil
}
func (s *detectionAccountRepoStub) ListSchedulableUngroupedByPlatforms(ctx context.Context, platforms []string) ([]Account, error) {
	return nil, nil
}
func (s *detectionAccountRepoStub) SetRateLimited(ctx context.Context, id int64, resetAt time.Time) error {
	return nil
}
func (s *detectionAccountRepoStub) SetModelRateLimit(ctx context.Context, id int64, scope string, resetAt time.Time) error {
	return nil
}
func (s *detectionAccountRepoStub) SetOverloaded(ctx context.Context, id int64, until time.Time) error {
	return nil
}
func (s *detectionAccountRepoStub) SetTempUnschedulable(ctx context.Context, id int64, until time.Time, reason string) error {
	return nil
}
func (s *detectionAccountRepoStub) ClearTempUnschedulable(ctx context.Context, id int64) error { return nil }
func (s *detectionAccountRepoStub) ClearRateLimit(ctx context.Context, id int64) error           { return nil }
func (s *detectionAccountRepoStub) ClearAntigravityQuotaScopes(ctx context.Context, id int64) error {
	return nil
}
func (s *detectionAccountRepoStub) ClearModelRateLimits(ctx context.Context, id int64) error { return nil }
func (s *detectionAccountRepoStub) UpdateSessionWindow(ctx context.Context, id int64, start, end *time.Time, status string) error {
	return nil
}
func (s *detectionAccountRepoStub) UpdateExtra(ctx context.Context, id int64, updates map[string]any) error {
	if s.updateExtra == nil {
		s.updateExtra = map[int64]map[string]any{}
	}
	copied := make(map[string]any, len(updates))
	for key, value := range updates {
		copied[key] = value
	}
	s.updateExtra[id] = copied
	return nil
}
func (s *detectionAccountRepoStub) BulkUpdate(ctx context.Context, ids []int64, updates AccountBulkUpdate) (int64, error) {
	return 0, nil
}
func (s *detectionAccountRepoStub) IncrementQuotaUsed(ctx context.Context, id int64, amount float64) error {
	return nil
}
func (s *detectionAccountRepoStub) ResetQuotaUsed(ctx context.Context, id int64) error { return nil }

func TestOpenAIOAuthDetectionService_ProbeAccounts401SetsErrorAndMetadata(t *testing.T) {
	repo := &detectionAccountRepoStub{
		accountsByID: map[int64]*Account{
			1: {
				ID:       1,
				Platform: PlatformOpenAI,
				Type:     AccountTypeOAuth,
				Status:   StatusActive,
				Credentials: map[string]any{
					"access_token":       "test-token",
					"chatgpt_account_id": "acct-1",
				},
			},
		},
	}
	upstream := &detectionHTTPUpstreamStub{
		response: &http.Response{
			StatusCode: http.StatusUnauthorized,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"error":{"code":"token_revoked","message":"revoked"}}`)),
		},
	}

	svc := NewOpenAIOAuthDetectionService(repo, upstream, nil)
	result, err := svc.ProbeAccounts(context.Background(), []int64{1}, "gpt-5.4-mini")
	require.NoError(t, err)
	require.Equal(t, 1, result.Checked)
	require.Equal(t, 1, result.Unauthorized)
	require.Len(t, repo.setError, 1)
	require.Equal(t, int64(1), repo.setError[0].id)
	require.Contains(t, repo.setError[0].msg, openAIOAuthDetectionErrorPrefix)
	require.Contains(t, repo.setError[0].msg, "token_revoked")
	require.NotEmpty(t, upstream.requests)
	body, _ := io.ReadAll(upstream.requests[0].Body)
	require.Contains(t, string(body), `"model":"gpt-5.4-mini"`)
	require.Equal(t, openAIOAuthDetectionResultUnauthorized, repo.updateExtra[1][openAIOAuthDetectionExtraResultKey])
	require.Equal(t, "gpt-5.4-mini", repo.updateExtra[1][openAIOAuthDetectionExtraModelKey])
	require.Contains(t, repo.updateExtra[1][openAIOAuthDetectionExtraReasonKey], "token_revoked")
}

func TestOpenAIOAuthDetectionService_ProbeAccountsSuccessClearsOwn401ErrorOnly(t *testing.T) {
	t.Run("clears own detection error", func(t *testing.T) {
		repo := &detectionAccountRepoStub{
			accountsByID: map[int64]*Account{
				1: {
					ID:           1,
					Platform:     PlatformOpenAI,
					Type:         AccountTypeOAuth,
					Status:       StatusError,
					ErrorMessage: openAIOAuthDetectionErrorPrefix + "old 401",
					Credentials: map[string]any{
						"access_token": "test-token",
					},
				},
			},
		}
		upstream := &detectionHTTPUpstreamStub{
			response: &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			},
		}

		svc := NewOpenAIOAuthDetectionService(repo, upstream, nil)
		result, err := svc.ProbeAccounts(context.Background(), []int64{1}, "")
		require.NoError(t, err)
		require.Equal(t, 1, result.Healthy)
		require.Equal(t, []int64{1}, repo.clearError)
		require.Equal(t, openAIOAuthDetectionResultHealthy, repo.updateExtra[1][openAIOAuthDetectionExtraResultKey])
		require.Equal(t, openAIOAuthDetectionDefaultModel, repo.updateExtra[1][openAIOAuthDetectionExtraModelKey])
		require.Equal(t, "", repo.updateExtra[1][openAIOAuthDetectionExtraReasonKey])
	})

	t.Run("does not clear foreign error", func(t *testing.T) {
		repo := &detectionAccountRepoStub{
			accountsByID: map[int64]*Account{
				2: {
					ID:           2,
					Platform:     PlatformOpenAI,
					Type:         AccountTypeOAuth,
					Status:       StatusError,
					ErrorMessage: "manual error",
					Credentials: map[string]any{
						"access_token": "test-token",
					},
				},
			},
		}
		upstream := &detectionHTTPUpstreamStub{
			response: &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			},
		}

		svc := NewOpenAIOAuthDetectionService(repo, upstream, nil)
		_, err := svc.ProbeAccounts(context.Background(), []int64{2}, "")
		require.NoError(t, err)
		require.Empty(t, repo.clearError)
	})
}

func TestOpenAIOAuthBodyHas401(t *testing.T) {
	require.True(t, openAIOAuthBodyHas401(http.StatusUnauthorized, ""))
	require.True(t, openAIOAuthBodyHas401(http.StatusBadRequest, `{"status":401}`))
	require.True(t, openAIOAuthBodyHas401(http.StatusBadGateway, `unauthorized`))
	require.False(t, openAIOAuthBodyHas401(http.StatusBadRequest, `{"status":400}`))

	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(`{"status":401}`), &payload))
}
