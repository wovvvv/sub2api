package service

import (
	"context"
	"errors"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type workerOAuthRefresherStub struct {
	info         *OpenAITokenInfo
	err          error
	lastToken    string
	lastProxyURL string
	lastClientID string
}

func (s *workerOAuthRefresherStub) RefreshTokenWithClientID(ctx context.Context, refreshToken, proxyURL, clientID string) (*OpenAITokenInfo, error) {
	s.lastToken = refreshToken
	s.lastProxyURL = proxyURL
	s.lastClientID = clientID
	return s.info, s.err
}

func (s *workerOAuthRefresherStub) BuildAccountCredentials(tokenInfo *OpenAITokenInfo) map[string]any {
	if tokenInfo == nil {
		return map[string]any{}
	}
	creds := map[string]any{
		"access_token": tokenInfo.AccessToken,
	}
	if tokenInfo.RefreshToken != "" {
		creds["refresh_token"] = tokenInfo.RefreshToken
	}
	if tokenInfo.IDToken != "" {
		creds["id_token"] = tokenInfo.IDToken
	}
	if tokenInfo.Email != "" {
		creds["email"] = tokenInfo.Email
	}
	if tokenInfo.ChatGPTAccountID != "" {
		creds["chatgpt_account_id"] = tokenInfo.ChatGPTAccountID
	}
	if tokenInfo.ClientID != "" {
		creds["client_id"] = tokenInfo.ClientID
	}
	return creds
}

type workerAccountReaderStub struct {
	accounts []Account
	err      error
}

func (s *workerAccountReaderStub) ListByPlatform(ctx context.Context, platform string) ([]Account, error) {
	return s.accounts, s.err
}

type workerAccountCreatorStub struct {
	account      *Account
	err          error
	createdInput *CreateAccountInput
}

func (s *workerAccountCreatorStub) CreateAccount(ctx context.Context, input *CreateAccountInput) (*Account, error) {
	s.createdInput = input
	if s.err != nil {
		return nil, s.err
	}
	if s.account != nil {
		return s.account, nil
	}
	return &Account{ID: 101}, nil
}

type workerProxyReaderStub struct {
	proxy   *Proxy
	err     error
	lastID  int64
	called  bool
}

func (s *workerProxyReaderStub) GetProxy(ctx context.Context, id int64) (*Proxy, error) {
	s.called = true
	s.lastID = id
	return s.proxy, s.err
}

type workerModelFetcherStub struct {
	models []string
	err    error
}

func (s *workerModelFetcherStub) Fetch(ctx context.Context, accessToken string) ([]string, error) {
	return s.models, s.err
}

func TestAccountRegistrationWorkerImportOpenAI(t *testing.T) {
	ctx := context.Background()

	baseInput := AccountRegistrationWorkerOpenAIInput{
		Email:        "worker@example.com",
		RefreshToken: "rt-123",
		AccessToken:  "at-existing",
		IDToken:      "id-123",
		AccountID:    "acct-worker",
		ClientID:     "client-worker",
		ImportOptions: AccountRegistrationWorkerImportOptions{
			GroupIDs:       []int64{9, 10},
			Concurrency:    testIntPtr(1),
			LoadFactor:     testIntPtr(1),
			Priority:       testIntPtr(2),
			RateMultiplier: testFloat64Ptr(1),
		},
	}

	t.Run("missing group ids rejected", func(t *testing.T) {
		svc := NewAccountRegistrationWorkerService(
			"worker-secret",
			&workerOAuthRefresherStub{},
			&workerAccountReaderStub{},
			&workerAccountCreatorStub{},
			&workerProxyReaderStub{},
			&workerModelFetcherStub{},
		)

		input := baseInput
		input.ImportOptions.GroupIDs = nil
		_, err := svc.ImportOpenAI(ctx, input)
		require.Error(t, err)
		statusCode, status := infraerrors.ToHTTP(err)
		require.Equal(t, 400, statusCode)
		require.Equal(t, "ACCOUNT_REGISTRATION_GROUPS_REQUIRED", status.Reason)
	})

	t.Run("refresh failure rejected", func(t *testing.T) {
		svc := NewAccountRegistrationWorkerService(
			"worker-secret",
			&workerOAuthRefresherStub{err: errors.New("refresh failed")},
			&workerAccountReaderStub{},
			&workerAccountCreatorStub{},
			&workerProxyReaderStub{},
			&workerModelFetcherStub{},
		)

		_, err := svc.ImportOpenAI(ctx, baseInput)
		require.Error(t, err)
	})

	t.Run("duplicate email rejected", func(t *testing.T) {
		svc := NewAccountRegistrationWorkerService(
			"worker-secret",
			&workerOAuthRefresherStub{info: &OpenAITokenInfo{
				AccessToken:      "at-new",
				RefreshToken:     "rt-new",
				Email:            "worker@example.com",
				ChatGPTAccountID: "acct-worker-new",
			}},
			&workerAccountReaderStub{accounts: []Account{
				{Credentials: map[string]any{"email": "worker@example.com"}},
			}},
			&workerAccountCreatorStub{},
			&workerProxyReaderStub{},
			&workerModelFetcherStub{},
		)

		_, err := svc.ImportOpenAI(ctx, baseInput)
		require.Error(t, err)
		statusCode, status := infraerrors.ToHTTP(err)
		require.Equal(t, 409, statusCode)
		require.Equal(t, "ACCOUNT_REGISTRATION_DUPLICATE_ACCOUNT", status.Reason)
	})

	t.Run("whitelist without import models writes direct model mapping", func(t *testing.T) {
		creator := &workerAccountCreatorStub{account: &Account{ID: 102}}
		svc := NewAccountRegistrationWorkerService(
			"worker-secret",
			&workerOAuthRefresherStub{info: &OpenAITokenInfo{
				AccessToken:      "at-new",
				RefreshToken:     "rt-new",
				IDToken:          "id-new",
				Email:            "worker@example.com",
				ChatGPTAccountID: "acct-worker-new",
				ClientID:         "client-worker",
			}},
			&workerAccountReaderStub{},
			creator,
			&workerProxyReaderStub{},
			&workerModelFetcherStub{},
		)

		input := baseInput
		input.ImportOptions.ModelWhitelist = []string{"gpt-5.1-codex", "gpt-5.4"}
		result, err := svc.ImportOpenAI(ctx, input)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, creator.createdInput)
		require.Equal(t, []int64{9, 10}, creator.createdInput.GroupIDs)
		require.Equal(t, "codex:worker@example.com", creator.createdInput.Name)
		require.Equal(t, map[string]string{
			"gpt-5.1-codex": "gpt-5.1-codex",
			"gpt-5.4":       "gpt-5.4",
		}, creator.createdInput.Credentials["model_mapping"])
	})

	t.Run("import models with whitelist writes intersection mapping", func(t *testing.T) {
		creator := &workerAccountCreatorStub{account: &Account{ID: 103}}
		svc := NewAccountRegistrationWorkerService(
			"worker-secret",
			&workerOAuthRefresherStub{info: &OpenAITokenInfo{
				AccessToken:      "at-new",
				RefreshToken:     "rt-new",
				Email:            "worker@example.com",
				ChatGPTAccountID: "acct-worker-new",
				ClientID:         "client-worker",
			}},
			&workerAccountReaderStub{},
			creator,
			&workerProxyReaderStub{},
			&workerModelFetcherStub{models: []string{"gpt-5.4", "gpt-4.1", "o3"}},
		)

		input := baseInput
		input.ImportOptions.ImportModels = true
		input.ImportOptions.ModelWhitelist = []string{"gpt-5.4", "gpt-5.1-codex"}
		_, err := svc.ImportOpenAI(ctx, input)
		require.NoError(t, err)
		require.Equal(t, map[string]string{
			"gpt-5.4": "gpt-5.4",
		}, creator.createdInput.Credentials["model_mapping"])
	})

	t.Run("maps import settings to create account input and resolves proxy", func(t *testing.T) {
		creator := &workerAccountCreatorStub{account: &Account{ID: 104}}
		proxyReader := &workerProxyReaderStub{
			proxy: &Proxy{
				ID:       7,
				Protocol: "http",
				Host:     "127.0.0.1",
				Port:     7890,
			},
		}
		oauth := &workerOAuthRefresherStub{info: &OpenAITokenInfo{
			AccessToken:      "at-new",
			RefreshToken:     "rt-new",
			Email:            "worker@example.com",
			ChatGPTAccountID: "acct-worker-new",
			ClientID:         "client-worker",
		}}
		svc := NewAccountRegistrationWorkerService(
			"worker-secret",
			oauth,
			&workerAccountReaderStub{},
			creator,
			proxyReader,
			&workerModelFetcherStub{},
		)

		input := baseInput
		input.ImportOptions.ProxyID = testInt64Ptr(7)
		input.ImportOptions.Notes = "from worker"
		input.ImportOptions.Concurrency = testIntPtr(3)
		input.ImportOptions.LoadFactor = testIntPtr(4)
		input.ImportOptions.Priority = testIntPtr(5)
		input.ImportOptions.RateMultiplier = testFloat64Ptr(0.5)

		_, err := svc.ImportOpenAI(ctx, input)
		require.NoError(t, err)
		require.True(t, proxyReader.called)
		require.Equal(t, int64(7), proxyReader.lastID)
		require.Equal(t, "rt-123", oauth.lastToken)
		require.Equal(t, "http://127.0.0.1:7890", oauth.lastProxyURL)
		require.Equal(t, "client-worker", oauth.lastClientID)
		require.Equal(t, []int64{9, 10}, creator.createdInput.GroupIDs)
		require.Equal(t, testInt64Ptr(7), creator.createdInput.ProxyID)
		require.Equal(t, 3, creator.createdInput.Concurrency)
		require.Equal(t, testIntPtr(4), creator.createdInput.LoadFactor)
		require.Equal(t, 5, creator.createdInput.Priority)
		require.Equal(t, testFloat64Ptr(0.5), creator.createdInput.RateMultiplier)
		require.Equal(t, "from worker", *creator.createdInput.Notes)
		require.True(t, creator.createdInput.SkipDefaultGroupBind)
	})
}
