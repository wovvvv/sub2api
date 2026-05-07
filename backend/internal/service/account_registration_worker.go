package service

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	accountRegistrationWorkerUnauthorizedReason   = "ACCOUNT_REGISTRATION_WORKER_UNAUTHORIZED"
	accountRegistrationWorkerGroupsRequiredReason = "ACCOUNT_REGISTRATION_GROUPS_REQUIRED"
	accountRegistrationWorkerDuplicateReason      = "ACCOUNT_REGISTRATION_DUPLICATE_ACCOUNT"
	accountRegistrationWorkerProxyNotFoundReason  = "ACCOUNT_REGISTRATION_PROXY_NOT_FOUND"
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

type AccountRegistrationWorkerOpenAIInput struct {
	Email         string                                 `json:"email"`
	RefreshToken  string                                 `json:"refresh_token"`
	AccessToken   string                                 `json:"access_token,omitempty"`
	IDToken       string                                 `json:"id_token,omitempty"`
	AccountID     string                                 `json:"account_id,omitempty"`
	ClientID      string                                 `json:"client_id,omitempty"`
	ExpiresAt     string                                 `json:"expires_at,omitempty"`
	ImportOptions AccountRegistrationWorkerImportOptions `json:"import_options"`
}

type AccountRegistrationWorkerOpenAIResult struct {
	AccountID      int64             `json:"account_id"`
	AccountName    string            `json:"account_name"`
	ImportedModels []string          `json:"imported_models,omitempty"`
	ModelMapping   map[string]string `json:"model_mapping,omitempty"`
}

type AccountRegistrationWorkerOAuthRefresher interface {
	RefreshTokenWithClientID(ctx context.Context, refreshToken, proxyURL, clientID string) (*OpenAITokenInfo, error)
	BuildAccountCredentials(tokenInfo *OpenAITokenInfo) map[string]any
}

type AccountRegistrationWorkerAccountReader interface {
	ListByPlatform(ctx context.Context, platform string) ([]Account, error)
}

type AccountRegistrationWorkerAccountCreator interface {
	CreateAccount(ctx context.Context, input *CreateAccountInput) (*Account, error)
}

type AccountRegistrationWorkerProxyReader interface {
	GetProxy(ctx context.Context, id int64) (*Proxy, error)
}

type AccountRegistrationWorkerModelFetcher interface {
	Fetch(ctx context.Context, accessToken string) ([]string, error)
}

type AccountRegistrationWorkerService struct {
	workerAPIKey   string
	oauth          AccountRegistrationWorkerOAuthRefresher
	accountReader  AccountRegistrationWorkerAccountReader
	accountCreator AccountRegistrationWorkerAccountCreator
	proxyReader    AccountRegistrationWorkerProxyReader
	modelFetcher   AccountRegistrationWorkerModelFetcher
}

func NewAccountRegistrationWorkerService(
	workerAPIKey string,
	oauth AccountRegistrationWorkerOAuthRefresher,
	accountReader AccountRegistrationWorkerAccountReader,
	accountCreator AccountRegistrationWorkerAccountCreator,
	proxyReader AccountRegistrationWorkerProxyReader,
	modelFetcher AccountRegistrationWorkerModelFetcher,
) *AccountRegistrationWorkerService {
	return &AccountRegistrationWorkerService{
		workerAPIKey:   strings.TrimSpace(workerAPIKey),
		oauth:          oauth,
		accountReader:  accountReader,
		accountCreator: accountCreator,
		proxyReader:    proxyReader,
		modelFetcher:   modelFetcher,
	}
}

func (s *AccountRegistrationWorkerService) ImportOpenAIWithAuth(ctx context.Context, authHeader string, input AccountRegistrationWorkerOpenAIInput) (*AccountRegistrationWorkerOpenAIResult, error) {
	if err := s.verifyWorkerAuth(authHeader); err != nil {
		return nil, err
	}
	return s.ImportOpenAI(ctx, input)
}

func (s *AccountRegistrationWorkerService) ImportOpenAI(ctx context.Context, input AccountRegistrationWorkerOpenAIInput) (*AccountRegistrationWorkerOpenAIResult, error) {
	if s.oauth == nil {
		return nil, fmt.Errorf("account registration worker oauth refresher is not configured")
	}
	if s.accountCreator == nil {
		return nil, fmt.Errorf("account registration worker account creator is not configured")
	}
	groupIDs := normalizeAccountRegistrationWorkerGroupIDs(input.ImportOptions)
	if len(groupIDs) == 0 {
		return nil, infraerrors.BadRequest(accountRegistrationWorkerGroupsRequiredReason, "group_ids is required")
	}

	proxyURL := ""
	if input.ImportOptions.ProxyID != nil {
		if s.proxyReader == nil {
			return nil, fmt.Errorf("account registration worker proxy reader is not configured")
		}
		proxy, err := s.proxyReader.GetProxy(ctx, *input.ImportOptions.ProxyID)
		if err != nil || proxy == nil {
			return nil, infraerrors.BadRequest(accountRegistrationWorkerProxyNotFoundReason, "proxy not found")
		}
		proxyURL = proxy.URL()
	}

	tokenInfo, err := s.oauth.RefreshTokenWithClientID(
		ctx,
		strings.TrimSpace(input.RefreshToken),
		proxyURL,
		strings.TrimSpace(input.ClientID),
	)
	if err != nil {
		return nil, err
	}

	if tokenInfo == nil {
		return nil, fmt.Errorf("account registration worker received empty token info")
	}

	// Preserve upstream-provided identity fields when refresh response is sparse.
	if strings.TrimSpace(tokenInfo.Email) == "" {
		tokenInfo.Email = strings.TrimSpace(input.Email)
	}
	if strings.TrimSpace(tokenInfo.ChatGPTAccountID) == "" {
		tokenInfo.ChatGPTAccountID = strings.TrimSpace(input.AccountID)
	}
	if strings.TrimSpace(tokenInfo.ClientID) == "" {
		tokenInfo.ClientID = strings.TrimSpace(input.ClientID)
	}
	if strings.TrimSpace(tokenInfo.IDToken) == "" {
		tokenInfo.IDToken = strings.TrimSpace(input.IDToken)
	}

	if err := s.ensureNoDuplicateOpenAIAccount(ctx, tokenInfo.Email, tokenInfo.ChatGPTAccountID); err != nil {
		return nil, err
	}

	credentials := s.oauth.BuildAccountCredentials(tokenInfo)
	if credentials == nil {
		credentials = map[string]any{}
	}
	if _, ok := credentials["id_token"]; !ok && strings.TrimSpace(input.IDToken) != "" {
		credentials["id_token"] = strings.TrimSpace(input.IDToken)
	}
	if _, ok := credentials["email"]; !ok && strings.TrimSpace(input.Email) != "" {
		credentials["email"] = strings.TrimSpace(input.Email)
	}
	if _, ok := credentials["chatgpt_account_id"]; !ok && strings.TrimSpace(input.AccountID) != "" {
		credentials["chatgpt_account_id"] = strings.TrimSpace(input.AccountID)
	}
	if _, ok := credentials["account_id"]; !ok && strings.TrimSpace(input.AccountID) != "" {
		credentials["account_id"] = strings.TrimSpace(input.AccountID)
	}
	if _, ok := credentials["client_id"]; !ok && strings.TrimSpace(input.ClientID) != "" {
		credentials["client_id"] = strings.TrimSpace(input.ClientID)
	}

	modelMapping, importedModels := s.resolveModelMapping(ctx, tokenInfo.AccessToken, input.ImportOptions)
	if len(modelMapping) > 0 {
		credentials["model_mapping"] = modelMapping
	}

	accountNameEmail := strings.TrimSpace(tokenInfo.Email)
	if accountNameEmail == "" {
		accountNameEmail = strings.TrimSpace(input.Email)
	}
	accountName := "codex:" + accountNameEmail

	var notesPtr *string
	if notes := strings.TrimSpace(input.ImportOptions.Notes); notes != "" {
		notesPtr = &notes
	}

	concurrency, loadFactor, priority, rateMultiplier := normalizeAccountRegistrationWorkerOptions(input.ImportOptions)

	account, err := s.accountCreator.CreateAccount(ctx, &CreateAccountInput{
		Name:                 accountName,
		Notes:                notesPtr,
		Platform:             PlatformOpenAI,
		Type:                 AccountTypeOAuth,
		Credentials:          credentials,
		ProxyID:              input.ImportOptions.ProxyID,
		Concurrency:          concurrency,
		LoadFactor:           loadFactor,
		Priority:             priority,
		RateMultiplier:       rateMultiplier,
		GroupIDs:             groupIDs,
		SkipDefaultGroupBind: true,
	})
	if err != nil {
		return nil, err
	}

	return &AccountRegistrationWorkerOpenAIResult{
		AccountID:      account.ID,
		AccountName:    accountName,
		ImportedModels: importedModels,
		ModelMapping:   modelMapping,
	}, nil
}

func (s *AccountRegistrationWorkerService) verifyWorkerAuth(authHeader string) error {
	expected := strings.TrimSpace(s.workerAPIKey)
	if expected == "" {
		return infraerrors.Forbidden(accountRegistrationWorkerUnauthorizedReason, "worker auth is not configured")
	}
	provided := strings.TrimSpace(authHeader)
	if !strings.HasPrefix(strings.ToLower(provided), "bearer ") {
		return infraerrors.Forbidden(accountRegistrationWorkerUnauthorizedReason, "invalid worker token")
	}
	token := strings.TrimSpace(provided[len("Bearer "):])
	if subtle.ConstantTimeCompare([]byte(token), []byte(expected)) != 1 {
		return infraerrors.Forbidden(accountRegistrationWorkerUnauthorizedReason, "invalid worker token")
	}
	return nil
}

func (s *AccountRegistrationWorkerService) ensureNoDuplicateOpenAIAccount(ctx context.Context, email, accountID string) error {
	if s.accountReader == nil {
		return nil
	}
	accounts, err := s.accountReader.ListByPlatform(ctx, PlatformOpenAI)
	if err != nil {
		return err
	}
	index := buildCodexRegistrationDuplicateIndex(accounts)
	if normalizedEmail := normalizeCodexRegistrationIdentity(email); normalizedEmail != "" {
		if _, exists := index.emails[normalizedEmail]; exists {
			return infraerrors.New(http.StatusConflict, accountRegistrationWorkerDuplicateReason, "duplicate email already exists")
		}
	}
	if normalizedAccountID := normalizeCodexRegistrationIdentity(accountID); normalizedAccountID != "" {
		if _, exists := index.accountIDs[normalizedAccountID]; exists {
			return infraerrors.New(http.StatusConflict, accountRegistrationWorkerDuplicateReason, "duplicate account already exists")
		}
	}
	return nil
}

func (s *AccountRegistrationWorkerService) resolveModelMapping(ctx context.Context, accessToken string, opts AccountRegistrationWorkerImportOptions) (map[string]string, []string) {
	whitelistSet := make(map[string]struct{}, len(opts.ModelWhitelist))
	for _, model := range opts.ModelWhitelist {
		trimmed := strings.TrimSpace(model)
		if trimmed == "" {
			continue
		}
		whitelistSet[trimmed] = struct{}{}
	}

	if !opts.ImportModels {
		if len(whitelistSet) == 0 {
			return nil, nil
		}
		return workerModelMappingFromList(workerSortedModelList(whitelistSet)), workerSortedModelList(whitelistSet)
	}

	models := []string{}
	if s.modelFetcher != nil && strings.TrimSpace(accessToken) != "" {
		fetched, err := s.modelFetcher.Fetch(ctx, accessToken)
		if err == nil {
			models = fetched
		}
	}

	modelSet := make(map[string]struct{}, len(models))
	for _, model := range models {
		trimmed := strings.TrimSpace(model)
		if trimmed == "" {
			continue
		}
		modelSet[trimmed] = struct{}{}
	}

	if len(modelSet) == 0 {
		if len(whitelistSet) == 0 {
			return nil, nil
		}
		return workerModelMappingFromList(workerSortedModelList(whitelistSet)), workerSortedModelList(whitelistSet)
	}

	if len(whitelistSet) == 0 {
		sorted := workerSortedModelList(modelSet)
		return workerModelMappingFromList(sorted), sorted
	}

	intersection := make([]string, 0, len(modelSet))
	for model := range modelSet {
		if _, ok := whitelistSet[model]; ok {
			intersection = append(intersection, model)
		}
	}
	sort.Strings(intersection)
	if len(intersection) == 0 {
		return nil, nil
	}
	return workerModelMappingFromList(intersection), intersection
}

func workerSortedModelList(set map[string]struct{}) []string {
	models := make([]string, 0, len(set))
	for model := range set {
		models = append(models, model)
	}
	sort.Strings(models)
	return models
}

func workerModelMappingFromList(models []string) map[string]string {
	if len(models) == 0 {
		return nil
	}
	mapping := make(map[string]string, len(models))
	for _, model := range models {
		mapping[model] = model
	}
	return mapping
}

func normalizeAccountRegistrationWorkerGroupIDs(opts AccountRegistrationWorkerImportOptions) []int64 {
	seen := make(map[int64]struct{}, len(opts.GroupIDs)+1)
	groupIDs := make([]int64, 0, len(opts.GroupIDs)+1)
	if opts.GroupID != nil && *opts.GroupID > 0 {
		seen[*opts.GroupID] = struct{}{}
		groupIDs = append(groupIDs, *opts.GroupID)
	}
	for _, groupID := range opts.GroupIDs {
		if groupID <= 0 {
			continue
		}
		if _, ok := seen[groupID]; ok {
			continue
		}
		seen[groupID] = struct{}{}
		groupIDs = append(groupIDs, groupID)
	}
	sort.Slice(groupIDs, func(i, j int) bool { return groupIDs[i] < groupIDs[j] })
	return groupIDs
}

func normalizeAccountRegistrationWorkerOptions(opts AccountRegistrationWorkerImportOptions) (int, *int, int, *float64) {
	concurrency := 1
	if opts.Concurrency != nil && *opts.Concurrency > 0 {
		concurrency = *opts.Concurrency
	}

	loadFactorValue := 1
	if opts.LoadFactor != nil && *opts.LoadFactor > 0 {
		loadFactorValue = *opts.LoadFactor
	}
	loadFactor := &loadFactorValue

	priority := 2
	if opts.Priority != nil && *opts.Priority > 0 {
		priority = *opts.Priority
	}

	rateMultiplierValue := 1.0
	if opts.RateMultiplier != nil {
		rateMultiplierValue = *opts.RateMultiplier
	}
	rateMultiplier := &rateMultiplierValue

	return concurrency, loadFactor, priority, rateMultiplier
}

type accountRegistrationWorkerModelFetcher struct {
	baseURL string
	client  *http.Client
}

func NewAccountRegistrationWorkerModelFetcher() AccountRegistrationWorkerModelFetcher {
	return &accountRegistrationWorkerModelFetcher{
		baseURL: "https://api.openai.com/v1/models",
		client:  &http.Client{},
	}
}

func (f *accountRegistrationWorkerModelFetcher) Fetch(ctx context.Context, accessToken string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.baseURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(accessToken))
	req.Header.Set("Accept", "application/json")
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch models failed: status %d", resp.StatusCode)
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	modelSet := make(map[string]struct{}, len(payload.Data))
	for _, item := range payload.Data {
		if trimmed := strings.TrimSpace(item.ID); trimmed != "" {
			modelSet[trimmed] = struct{}{}
		}
	}
	return workerSortedModelList(modelSet), nil
}

func NewAccountRegistrationWorkerModelFetcherWithProxy(proxyURL string) AccountRegistrationWorkerModelFetcher {
	transport := &http.Transport{}
	if trimmed := strings.TrimSpace(proxyURL); trimmed != "" {
		if parsed, err := url.Parse(trimmed); err == nil {
			transport.Proxy = http.ProxyURL(parsed)
		}
	}
	return &accountRegistrationWorkerModelFetcher{
		baseURL: "https://api.openai.com/v1/models",
		client:  &http.Client{Transport: transport},
	}
}
