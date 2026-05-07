package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
)

const (
	openAIOAuthDetectionExtraCheckedAtKey = "openai_oauth_detection_last_checked_at"
	openAIOAuthDetectionExtraResultKey    = "openai_oauth_detection_last_result"
	openAIOAuthDetectionExtraReasonKey    = "openai_oauth_detection_last_reason"
	openAIOAuthDetectionExtraModelKey     = "openai_oauth_detection_last_model"

	openAIOAuthDetectionResultHealthy      = "healthy"
	openAIOAuthDetectionResultUnauthorized = "unauthorized"
	openAIOAuthDetectionResultProbeError   = "probe_error"
	openAIOAuthDetectionResultHTTPError    = "http_error"

	openAIOAuthDetectionErrorPrefix  = "OpenAI OAuth detection (401): "
	openAIOAuthDetectionBodyLimit    = 4096
	openAIOAuthDetectionDefaultModel = "gpt-5.4-mini"
)

type OpenAIOAuthDetectionBatchResult struct {
	Checked      int              `json:"checked"`
	Healthy      int              `json:"healthy"`
	Unauthorized int              `json:"unauthorized"`
	Failed       map[int64]string `json:"failed"`
}

type OpenAIOAuthDetectionService struct {
	accountRepo         AccountRepository
	httpUpstream        HTTPUpstream
	tlsFPProfileService *TLSFingerprintProfileService
	now                 func() time.Time
}

func NewOpenAIOAuthDetectionService(
	accountRepo AccountRepository,
	httpUpstream HTTPUpstream,
	tlsFPProfileService *TLSFingerprintProfileService,
) *OpenAIOAuthDetectionService {
	return &OpenAIOAuthDetectionService{
		accountRepo:         accountRepo,
		httpUpstream:        httpUpstream,
		tlsFPProfileService: tlsFPProfileService,
		now:                 time.Now,
	}
}

func (s *OpenAIOAuthDetectionService) ProbeAccounts(ctx context.Context, accountIDs []int64, model string) (*OpenAIOAuthDetectionBatchResult, error) {
	result := &OpenAIOAuthDetectionBatchResult{
		Failed: make(map[int64]string),
	}
	ids := uniqueInt64s(accountIDs)
	if len(ids) == 0 {
		return result, nil
	}
	if s == nil || s.accountRepo == nil {
		return nil, fmt.Errorf("openai oauth detection account repository is not configured")
	}
	if s.httpUpstream == nil {
		return nil, fmt.Errorf("openai oauth detection upstream is not configured")
	}

	accounts, err := s.accountRepo.GetByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	accountByID := make(map[int64]*Account, len(accounts))
	for _, account := range accounts {
		if account != nil {
			accountByID[account.ID] = account
		}
	}

	for _, id := range ids {
		account := accountByID[id]
		if account == nil {
			result.Failed[id] = "account not found"
			continue
		}
		if !account.IsOpenAIOAuth() {
			result.Failed[id] = "account is not an OpenAI OAuth account"
			continue
		}

		result.Checked++
		probeErr := s.probeAccount(ctx, account, model)
		switch probeErr.kind {
		case openAIOAuthDetectionResultHealthy:
			result.Healthy++
		case openAIOAuthDetectionResultUnauthorized:
			result.Unauthorized++
		default:
			if probeErr.reason != "" {
				result.Failed[id] = probeErr.reason
			}
		}
	}

	return result, nil
}

type openAIOAuthDetectionProbeResult struct {
	kind   string
	reason string
}

func (s *OpenAIOAuthDetectionService) probeAccount(ctx context.Context, account *Account, model string) openAIOAuthDetectionProbeResult {
	checkedAt := s.now().UTC()
	probeModel := strings.TrimSpace(model)
	if probeModel == "" {
		probeModel = openAIOAuthDetectionDefaultModel
	}
	accessToken := strings.TrimSpace(account.GetOpenAIAccessToken())
	if accessToken == "" {
		reason := "missing access token"
		s.updateDetectionMetadata(ctx, account.ID, probeModel, openAIOAuthDetectionResultProbeError, reason, checkedAt)
		return openAIOAuthDetectionProbeResult{kind: openAIOAuthDetectionResultProbeError, reason: reason}
	}

	payload := createOpenAITestPayload(probeModel, true)
	payloadBytes, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, chatgptCodexAPIURL, bytes.NewReader(payloadBytes))
	if err != nil {
		reason := fmt.Sprintf("create request failed: %v", err)
		s.updateDetectionMetadata(ctx, account.ID, probeModel, openAIOAuthDetectionResultProbeError, reason, checkedAt)
		return openAIOAuthDetectionProbeResult{kind: openAIOAuthDetectionResultProbeError, reason: reason}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("accept", "text/event-stream")
	req.Host = "chatgpt.com"
	if chatgptAccountID := strings.TrimSpace(account.GetChatGPTAccountID()); chatgptAccountID != "" {
		req.Header.Set("chatgpt-account-id", chatgptAccountID)
	}

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

	resp, err := s.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, s.resolveTLSProfile(account))
	if err != nil {
		reason := fmt.Sprintf("probe request failed: %v", err)
		s.updateDetectionMetadata(ctx, account.ID, probeModel, openAIOAuthDetectionResultProbeError, reason, checkedAt)
		return openAIOAuthDetectionProbeResult{kind: openAIOAuthDetectionResultProbeError, reason: reason}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		s.updateDetectionMetadata(ctx, account.ID, probeModel, openAIOAuthDetectionResultHealthy, "", checkedAt)
		if shouldClearOpenAIOAuthDetectionError(account) {
			_ = s.accountRepo.ClearError(ctx, account.ID)
		}
		return openAIOAuthDetectionProbeResult{kind: openAIOAuthDetectionResultHealthy}
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, openAIOAuthDetectionBodyLimit))
	reason := strings.TrimSpace(string(body))
	if reason == "" {
		reason = "empty upstream response"
	}
	if openAIOAuthBodyHas401(resp.StatusCode, reason) {
		errorMsg := openAIOAuthDetectionErrorPrefix + reason
		_ = s.accountRepo.SetError(ctx, account.ID, errorMsg)
		s.updateDetectionMetadata(ctx, account.ID, probeModel, openAIOAuthDetectionResultUnauthorized, reason, checkedAt)
		return openAIOAuthDetectionProbeResult{kind: openAIOAuthDetectionResultUnauthorized, reason: reason}
	}

	httpReason := fmt.Sprintf("upstream returned %d: %s", resp.StatusCode, reason)
	s.updateDetectionMetadata(ctx, account.ID, probeModel, openAIOAuthDetectionResultHTTPError, httpReason, checkedAt)
	return openAIOAuthDetectionProbeResult{kind: openAIOAuthDetectionResultHTTPError, reason: httpReason}
}

func (s *OpenAIOAuthDetectionService) updateDetectionMetadata(ctx context.Context, accountID int64, model, result, reason string, checkedAt time.Time) {
	if s == nil || s.accountRepo == nil || accountID <= 0 {
		return
	}
	updates := map[string]any{
		openAIOAuthDetectionExtraCheckedAtKey: checkedAt.Format(time.RFC3339),
		openAIOAuthDetectionExtraModelKey:     model,
		openAIOAuthDetectionExtraResultKey:    result,
		openAIOAuthDetectionExtraReasonKey:    reason,
	}
	_ = s.accountRepo.UpdateExtra(ctx, accountID, updates)
}

func (s *OpenAIOAuthDetectionService) resolveTLSProfile(account *Account) *tlsfingerprint.Profile {
	if s == nil || s.tlsFPProfileService == nil {
		return nil
	}
	return s.tlsFPProfileService.ResolveTLSProfile(account)
}

func shouldClearOpenAIOAuthDetectionError(account *Account) bool {
	if account == nil || account.Status != StatusError {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(account.ErrorMessage), openAIOAuthDetectionErrorPrefix)
}

func openAIOAuthBodyHas401(statusCode int, body string) bool {
	if statusCode == http.StatusUnauthorized {
		return true
	}
	lowerBody := strings.ToLower(strings.TrimSpace(body))
	if lowerBody == "" {
		return false
	}
	if strings.Contains(lowerBody, "unauthorized") || strings.Contains(lowerBody, "invalid_token") ||
		strings.Contains(lowerBody, "token_revoked") || strings.Contains(lowerBody, "token_invalidated") {
		return true
	}
	if strings.Contains(lowerBody, `"status":401`) || strings.Contains(lowerBody, `"status": 401`) {
		return true
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err == nil {
		if intFromAny(payload["status"]) == http.StatusUnauthorized {
			return true
		}
		if errObj, ok := payload["error"].(map[string]any); ok && intFromAny(errObj["status"]) == http.StatusUnauthorized {
			return true
		}
	}
	return false
}

func uniqueInt64s(values []int64) []int64 {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(values))
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func intFromAny(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return int(i)
		}
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return i
		}
	}
	return 0
}
