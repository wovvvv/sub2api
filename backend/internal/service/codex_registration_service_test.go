package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func testInt64Ptr(v int64) *int64        { return &v }
func testTimePtr(v time.Time) *time.Time { return &v }
func testIntPtr(v int) *int              { return &v }
func testFloat64Ptr(v float64) *float64  { return &v }

func mustSourceFingerprint(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func mustSourceMtime(t *testing.T, path string) time.Time {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	return info.ModTime().UTC()
}

func TestCodexRegistrationCandidateStateTransitions(t *testing.T) {
	now := time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC)

	t.Run("enum domain", func(t *testing.T) {
		require.Equal(t, []CodexRegistrationLivenessStatus{
			CodexRegistrationLivenessAlive,
			CodexRegistrationLivenessDead,
			CodexRegistrationLivenessInvalid,
			CodexRegistrationLivenessError,
		}, CodexRegistrationLivenessStatuses())

		require.Equal(t, []CodexRegistrationWorkflowState{
			CodexRegistrationWorkflowDetected,
			CodexRegistrationWorkflowStaged,
			CodexRegistrationWorkflowDuplicate,
			CodexRegistrationWorkflowImported,
		}, CodexRegistrationWorkflowStates())
	})

	t.Run("same source_path upserts", func(t *testing.T) {
		store := map[string]CodexRegistrationCandidateState{}
		store = ApplyCodexRegistrationScan(store, []CodexRegistrationScanCandidate{
			{
				SourcePath:        "/tmp/codex/a.json",
				SourceFilename:    "a.json",
				SourceMtime:       now,
				SourceFingerprint: "fp-a-1",
				Email:             "a@example.com",
				AccountID:         "acct-a",
				Type:              "oauth",
				LivenessStatus:    CodexRegistrationLivenessAlive,
			},
		}, now)

		require.Len(t, store, 1)
		first := store["/tmp/codex/a.json"]
		require.Equal(t, CodexRegistrationWorkflowDetected, first.WorkflowState)
		require.Equal(t, "fp-a-1", first.SourceFingerprint)

		store = ApplyCodexRegistrationScan(store, []CodexRegistrationScanCandidate{
			{
				SourcePath:        "/tmp/codex/a.json",
				SourceFilename:    "a.json",
				SourceMtime:       now.Add(5 * time.Minute),
				SourceFingerprint: "fp-a-2",
				Email:             "a2@example.com",
				AccountID:         "acct-a",
				Type:              "oauth",
				LivenessStatus:    CodexRegistrationLivenessAlive,
			},
		}, now.Add(5*time.Minute))

		require.Len(t, store, 1)
		updated := store["/tmp/codex/a.json"]
		require.Equal(t, "fp-a-2", updated.SourceFingerprint)
		require.Equal(t, "a2@example.com", updated.Email)
	})

	t.Run("staged survives only unchanged healthy files", func(t *testing.T) {
		store := map[string]CodexRegistrationCandidateState{
			"/tmp/codex/staged.json": {
				SourcePath:        "/tmp/codex/staged.json",
				SourceFilename:    "staged.json",
				SourceMtime:       now,
				SourceFingerprint: "fp-stage-1",
				Email:             "staged@example.com",
				AccountID:         "acct-stage",
				Type:              "oauth",
				LivenessStatus:    CodexRegistrationLivenessAlive,
				WorkflowState:     CodexRegistrationWorkflowStaged,
			},
		}

		healthyUnchanged := ApplyCodexRegistrationScan(store, []CodexRegistrationScanCandidate{
			{
				SourcePath:        "/tmp/codex/staged.json",
				SourceFilename:    "staged.json",
				SourceMtime:       now.Add(1 * time.Minute),
				SourceFingerprint: "fp-stage-1",
				Email:             "staged@example.com",
				AccountID:         "acct-stage",
				Type:              "oauth",
				LivenessStatus:    CodexRegistrationLivenessAlive,
			},
		}, now.Add(1*time.Minute))
		require.Equal(t, CodexRegistrationWorkflowStaged, healthyUnchanged["/tmp/codex/staged.json"].WorkflowState)

		changed := ApplyCodexRegistrationScan(store, []CodexRegistrationScanCandidate{
			{
				SourcePath:        "/tmp/codex/staged.json",
				SourceFilename:    "staged.json",
				SourceMtime:       now.Add(2 * time.Minute),
				SourceFingerprint: "fp-stage-2",
				Email:             "staged@example.com",
				AccountID:         "acct-stage",
				Type:              "oauth",
				LivenessStatus:    CodexRegistrationLivenessAlive,
			},
		}, now.Add(2*time.Minute))
		require.Equal(t, CodexRegistrationWorkflowDetected, changed["/tmp/codex/staged.json"].WorkflowState)

		unhealthy := ApplyCodexRegistrationScan(store, []CodexRegistrationScanCandidate{
			{
				SourcePath:        "/tmp/codex/staged.json",
				SourceFilename:    "staged.json",
				SourceMtime:       now.Add(3 * time.Minute),
				SourceFingerprint: "fp-stage-1",
				Email:             "staged@example.com",
				AccountID:         "acct-stage",
				Type:              "oauth",
				LivenessStatus:    CodexRegistrationLivenessDead,
			},
		}, now.Add(3*time.Minute))
		require.Equal(t, CodexRegistrationWorkflowDetected, unhealthy["/tmp/codex/staged.json"].WorkflowState)
	})

	t.Run("imported stays imported on later scans", func(t *testing.T) {
		importedAt := now.Add(-time.Hour)
		store := map[string]CodexRegistrationCandidateState{
			"/tmp/codex/imported.json": {
				SourcePath:        "/tmp/codex/imported.json",
				SourceFilename:    "imported.json",
				SourceMtime:       now,
				SourceFingerprint: "fp-imported-1",
				Email:             "imported@example.com",
				AccountID:         "acct-imported",
				Type:              "oauth",
				LivenessStatus:    CodexRegistrationLivenessAlive,
				WorkflowState:     CodexRegistrationWorkflowImported,
				ImportedAccountID: testInt64Ptr(123),
				ImportedAt:        &importedAt,
				LastCheckedAt:     now,
				CreatedAt:         now.Add(-2 * time.Hour),
				UpdatedAt:         now,
			},
		}

		scanned := ApplyCodexRegistrationScan(store, []CodexRegistrationScanCandidate{
			{
				SourcePath:        "/tmp/codex/imported.json",
				SourceFilename:    "imported.json",
				SourceMtime:       now.Add(5 * time.Minute),
				SourceFingerprint: "fp-imported-2",
				Email:             "imported-new@example.com",
				AccountID:         "acct-imported",
				Type:              "oauth",
				LivenessStatus:    CodexRegistrationLivenessDead,
			},
		}, now.Add(5*time.Minute))

		c := scanned["/tmp/codex/imported.json"]
		require.Equal(t, CodexRegistrationWorkflowImported, c.WorkflowState)
		require.Equal(t, "fp-imported-2", c.SourceFingerprint)
		require.Equal(t, CodexRegistrationLivenessDead, c.LivenessStatus)
	})

	t.Run("duplicate detection during scan", func(t *testing.T) {
		store := map[string]CodexRegistrationCandidateState{}
		store = ApplyCodexRegistrationScan(store, []CodexRegistrationScanCandidate{
			{
				SourcePath:        "/tmp/codex/a.json",
				SourceFilename:    "a.json",
				SourceMtime:       now,
				SourceFingerprint: "fp-a",
				Email:             "a@example.com",
				AccountID:         "acct-dup",
				Type:              "oauth",
				LivenessStatus:    CodexRegistrationLivenessAlive,
			},
			{
				SourcePath:        "/tmp/codex/b.json",
				SourceFilename:    "b.json",
				SourceMtime:       now,
				SourceFingerprint: "fp-b",
				Email:             "b@example.com",
				AccountID:         "acct-dup",
				Type:              "oauth",
				LivenessStatus:    CodexRegistrationLivenessAlive,
			},
			{
				SourcePath:        "/tmp/codex/c.json",
				SourceFilename:    "c.json",
				SourceMtime:       now,
				SourceFingerprint: "fp-c",
				Email:             "c@example.com",
				AccountID:         "acct-unique",
				Type:              "oauth",
				LivenessStatus:    CodexRegistrationLivenessAlive,
			},
		}, now)

		require.Equal(t, CodexRegistrationWorkflowDuplicate, store["/tmp/codex/a.json"].WorkflowState)
		require.Equal(t, CodexRegistrationWorkflowDuplicate, store["/tmp/codex/b.json"].WorkflowState)
		require.Equal(t, CodexRegistrationWorkflowDetected, store["/tmp/codex/c.json"].WorkflowState)
	})

	t.Run("preserves created and import metadata across rescan", func(t *testing.T) {
		createdAt := now.Add(-2 * time.Hour)
		importedAt := now.Add(-time.Hour)
		store := map[string]CodexRegistrationCandidateState{
			"/tmp/codex/meta.json": {
				SourcePath:        "/tmp/codex/meta.json",
				SourceFilename:    "meta.json",
				SourceMtime:       now,
				SourceFingerprint: "fp-meta-1",
				Email:             "meta@example.com",
				AccountID:         "acct-meta",
				Type:              "oauth",
				LivenessStatus:    CodexRegistrationLivenessAlive,
				WorkflowState:     CodexRegistrationWorkflowImported,
				ImportedAccountID: testInt64Ptr(456),
				ImportedAt:        &importedAt,
				CreatedAt:         createdAt,
				UpdatedAt:         now,
				LastCheckedAt:     now,
			},
		}

		scanned := ApplyCodexRegistrationScan(store, []CodexRegistrationScanCandidate{
			{
				SourcePath:        "/tmp/codex/meta.json",
				SourceFilename:    "meta.json",
				SourceMtime:       now.Add(10 * time.Minute),
				SourceFingerprint: "fp-meta-2",
				Email:             "meta2@example.com",
				AccountID:         "acct-meta",
				Type:              "oauth",
				LivenessStatus:    CodexRegistrationLivenessAlive,
			},
		}, now.Add(10*time.Minute))

		candidate := scanned["/tmp/codex/meta.json"]
		require.Equal(t, createdAt, candidate.CreatedAt)
		require.NotNil(t, candidate.ImportedAccountID)
		require.Equal(t, int64(456), *candidate.ImportedAccountID)
		require.NotNil(t, candidate.ImportedAt)
		require.Equal(t, importedAt, *candidate.ImportedAt)
	})

	t.Run("stage and unstage transitions are centralized", func(t *testing.T) {
		base := CodexRegistrationCandidateState{
			SourcePath:     "/tmp/codex/stage.json",
			LivenessStatus: CodexRegistrationLivenessAlive,
			WorkflowState:  CodexRegistrationWorkflowDetected,
			UpdatedAt:      now,
		}

		staged, err := StageCodexRegistrationCandidate(base, now.Add(time.Minute))
		require.NoError(t, err)
		require.Equal(t, CodexRegistrationWorkflowStaged, staged.WorkflowState)

		unstaged, err := UnstageCodexRegistrationCandidate(staged, now.Add(2*time.Minute))
		require.NoError(t, err)
		require.Equal(t, CodexRegistrationWorkflowDetected, unstaged.WorkflowState)

		_, err = StageCodexRegistrationCandidate(CodexRegistrationCandidateState{
			LivenessStatus: CodexRegistrationLivenessDead,
			WorkflowState:  CodexRegistrationWorkflowDetected,
		}, now)
		require.Error(t, err)

		_, err = UnstageCodexRegistrationCandidate(CodexRegistrationCandidateState{
			LivenessStatus: CodexRegistrationLivenessAlive,
			WorkflowState:  CodexRegistrationWorkflowDetected,
		}, now)
		require.Error(t, err)
	})

	t.Run("mark imported requires metadata and preserves invariants", func(t *testing.T) {
		candidate := CodexRegistrationCandidateState{
			SourcePath:     "/tmp/codex/import.json",
			LivenessStatus: CodexRegistrationLivenessAlive,
			WorkflowState:  CodexRegistrationWorkflowStaged,
		}

		imported, err := MarkCodexRegistrationCandidateImported(candidate, 789, now.Add(time.Minute))
		require.NoError(t, err)
		require.Equal(t, CodexRegistrationWorkflowImported, imported.WorkflowState)
		require.NotNil(t, imported.ImportedAccountID)
		require.Equal(t, int64(789), *imported.ImportedAccountID)
		require.NotNil(t, imported.ImportedAt)

		_, err = MarkCodexRegistrationCandidateImported(candidate, 0, now.Add(time.Minute))
		require.Error(t, err)

		_, err = MarkCodexRegistrationCandidateImported(candidate, 789, time.Time{})
		require.Error(t, err)
	})
}

func TestCodexRegistrationScanAndProbe(t *testing.T) {
	t.Run("scan classification and duplicate detection", func(t *testing.T) {
		tempDir := t.TempDir()
		require.NoError(t, writeJSONFile(filepath.Join(tempDir, "alive.json"), map[string]any{
			"type":          "codex",
			"refresh_token": "rt-alive",
			"email":         "alive@example.com",
			"account_id":    "acct-alive",
			"client_id":     "client-from-source",
		}))
		require.NoError(t, writeJSONFile(filepath.Join(tempDir, "refresh-auth-failure.json"), map[string]any{
			"type":          "codex",
			"refresh_token": "rt-auth-fail",
			"access_token":  "at-fallback-alive",
			"email":         "dead-refresh@example.com",
			"account_id":    "acct-dead-refresh",
		}))
		require.NoError(t, writeJSONFile(filepath.Join(tempDir, "probe-401.json"), map[string]any{
			"type":          "codex",
			"refresh_token": "rt-probe-401",
			"email":         "dead-probe@example.com",
			"account_id":    "acct-dead-probe",
		}))
		require.NoError(t, writeJSONFile(filepath.Join(tempDir, "probe-429.json"), map[string]any{
			"type":          "codex",
			"refresh_token": "rt-probe-429",
			"email":         "rate-limited@example.com",
			"account_id":    "acct-rate-limited",
		}))
		require.NoError(t, writeJSONFile(filepath.Join(tempDir, "timeout.json"), map[string]any{
			"type":          "codex",
			"refresh_token": "rt-timeout",
			"email":         "timeout@example.com",
			"account_id":    "acct-timeout",
		}))
		require.NoError(t, writeJSONFile(filepath.Join(tempDir, "upstream-5xx.json"), map[string]any{
			"type":          "codex",
			"refresh_token": "rt-5xx",
			"email":         "upstream-5xx@example.com",
			"account_id":    "acct-5xx",
		}))
		require.NoError(t, writeJSONFile(filepath.Join(tempDir, "malformed-probe.json"), map[string]any{
			"type":          "codex",
			"refresh_token": "rt-malformed-probe",
			"email":         "malformed-probe@example.com",
			"account_id":    "acct-malformed-probe",
		}))
		require.NoError(t, writeJSONFile(filepath.Join(tempDir, "missing-required.json"), map[string]any{
			"type":       "codex",
			"email":      "missing@example.com",
			"account_id": "acct-missing",
		}))
		require.NoError(t, os.WriteFile(filepath.Join(tempDir, "malformed.json"), []byte("{"), 0o600))
		require.NoError(t, writeJSONFile(filepath.Join(tempDir, "duplicate-existing.json"), map[string]any{
			"type":          "codex",
			"refresh_token": "rt-dup-existing",
			"email":         "dup-existing@example.com",
			"account_id":    "acct-existing",
		}))
		require.NoError(t, writeJSONFile(filepath.Join(tempDir, "duplicate-refreshed-identity.json"), map[string]any{
			"type":          "codex",
			"refresh_token": "rt-dup-refreshed",
			"email":         "stale-source@example.com",
			"account_id":    "acct-stale-source",
		}))
		require.NoError(t, writeJSONFile(filepath.Join(tempDir, "ignored-non-codex.json"), map[string]any{
			"type":          "other",
			"refresh_token": "rt-ignore",
			"email":         "ignore@example.com",
			"account_id":    "acct-ignore",
		}))
		require.NoError(t, os.WriteFile(filepath.Join(tempDir, "ignored.txt"), []byte(`{"type":"codex"}`), 0o600))

		probeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/backend-api/codex/responses/compact", r.URL.Path)
			require.Equal(t, "application/json", r.Header.Get("Content-Type"))
			require.Equal(t, "codex_cli_rs/0.101.0", r.Header.Get("User-Agent"))
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			require.Equal(t, "ping", body["instructions"])
			token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			switch token {
			case "at-alive":
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"ok":true}`))
			case "at-probe-401":
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			case "at-probe-429":
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":{"type":"usage_limit_reached","message":"limit reached"}}`))
			case "at-fallback-alive":
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":{"type":"usage_limit_reached","message":"limit reached"}}`))
			case "at-timeout":
				time.Sleep(2 * time.Second)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"ok":true}`))
			case "at-5xx":
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(`{"error":"upstream"}`))
			case "at-malformed-probe":
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`not-json`))
			case "at-dup-existing":
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"ok":true}`))
			default:
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"unknown token"}`))
			}
		}))
		defer probeServer.Close()

		now := time.Now().UTC()
		repo := newCodexCandidateRepoStub()
		staleDetected := repo.mustInsert(CodexRegistrationCandidateState{
			SourcePath:        filepath.Join(tempDir, "stale-detected.json"),
			SourceFilename:    "stale-detected.json",
			SourceMtime:       now,
			SourceFingerprint: "fp-stale-detected",
			Email:             "stale-detected@example.com",
			AccountID:         "acct-stale-detected",
			Type:              "codex",
			LivenessStatus:    CodexRegistrationLivenessAlive,
			WorkflowState:     CodexRegistrationWorkflowDetected,
			LastCheckedAt:     now,
			CreatedAt:         now,
			UpdatedAt:         now,
		})
		staleImported := repo.mustInsert(CodexRegistrationCandidateState{
			SourcePath:        filepath.Join(tempDir, "stale-imported.json"),
			SourceFilename:    "stale-imported.json",
			SourceMtime:       now,
			SourceFingerprint: "fp-stale-imported",
			Email:             "stale-imported@example.com",
			AccountID:         "acct-stale-imported",
			Type:              "codex",
			LivenessStatus:    CodexRegistrationLivenessAlive,
			WorkflowState:     CodexRegistrationWorkflowImported,
			ImportedAccountID: testInt64Ptr(999),
			ImportedAt:        testTimePtr(now),
			LastCheckedAt:     now,
			CreatedAt:         now,
			UpdatedAt:         now,
		})
		accountReader := &codexAccountReaderStub{
			accounts: []Account{
				{
					ID:       999,
					Platform: PlatformOpenAI,
					Type:     AccountTypeOAuth,
					Credentials: map[string]any{
						"chatgpt_account_id": "acct-existing",
						"email":              "existing@example.com",
					},
				},
			},
		}
		oauth := &codexOAuthRefresherStub{
			outcomes: map[string]codexOAuthOutcome{
				"rt-alive": {
					info: &OpenAITokenInfo{
						AccessToken:      "at-alive",
						RefreshToken:     "rt-new-alive",
						ExpiresAt:        time.Now().Add(time.Hour).Unix(),
						Email:            "alive@example.com",
						ChatGPTAccountID: "acct-alive",
						ClientID:         "client-from-source",
					},
				},
				"rt-auth-fail": {
					err: infraerrors.New(http.StatusUnauthorized, "OPENAI_OAUTH_TOKEN_REFRESH_FAILED", "invalid_grant"),
				},
				"rt-probe-401": {
					info: &OpenAITokenInfo{
						AccessToken:      "at-probe-401",
						RefreshToken:     "rt-probe-401-new",
						ExpiresAt:        time.Now().Add(time.Hour).Unix(),
						Email:            "dead-probe@example.com",
						ChatGPTAccountID: "acct-dead-probe",
					},
				},
				"rt-probe-429": {
					info: &OpenAITokenInfo{
						AccessToken:      "at-probe-429",
						RefreshToken:     "rt-probe-429-new",
						ExpiresAt:        time.Now().Add(time.Hour).Unix(),
						Email:            "rate-limited@example.com",
						ChatGPTAccountID: "acct-rate-limited",
					},
				},
				"rt-timeout": {
					info: &OpenAITokenInfo{
						AccessToken:      "at-timeout",
						RefreshToken:     "rt-timeout-new",
						ExpiresAt:        time.Now().Add(time.Hour).Unix(),
						Email:            "timeout@example.com",
						ChatGPTAccountID: "acct-timeout",
					},
				},
				"rt-5xx": {
					info: &OpenAITokenInfo{
						AccessToken:      "at-5xx",
						RefreshToken:     "rt-5xx-new",
						ExpiresAt:        time.Now().Add(time.Hour).Unix(),
						Email:            "upstream-5xx@example.com",
						ChatGPTAccountID: "acct-5xx",
					},
				},
				"rt-malformed-probe": {
					info: &OpenAITokenInfo{
						AccessToken:      "at-malformed-probe",
						RefreshToken:     "rt-malformed-probe-new",
						ExpiresAt:        time.Now().Add(time.Hour).Unix(),
						Email:            "malformed-probe@example.com",
						ChatGPTAccountID: "acct-malformed-probe",
					},
				},
				"rt-dup-existing": {
					info: &OpenAITokenInfo{
						AccessToken:      "at-dup-existing",
						RefreshToken:     "rt-dup-existing-new",
						ExpiresAt:        time.Now().Add(time.Hour).Unix(),
						Email:            "dup-existing@example.com",
						ChatGPTAccountID: "acct-existing",
					},
				},
				"rt-dup-refreshed": {
					info: &OpenAITokenInfo{
						AccessToken:      "at-dup-existing",
						RefreshToken:     "rt-dup-refreshed-new",
						ExpiresAt:        time.Now().Add(time.Hour).Unix(),
						Email:            "existing@example.com",
						ChatGPTAccountID: "acct-existing",
					},
				},
			},
		}
		creator := &codexAccountCreatorStub{}

		cfg := &config.Config{
			CodexRegistration: config.CodexRegistrationConfig{
				ScanWorkers:         4,
				ProbeTimeoutSeconds: 1,
			},
		}
		svc := NewCodexRegistrationService(cfg, repo, accountReader, oauth, creator)
		svc.sourceDir = tempDir
		svc.probeURL = probeServer.URL + "/backend-api/codex/responses/compact"

		got, err := svc.Scan(context.Background(), "")
		require.NoError(t, err)

		byPath := make(map[string]CodexRegistrationCandidateState, len(got))
		for _, candidate := range got {
			byPath[candidate.SourcePath] = candidate
		}

		require.Equal(t, CodexRegistrationLivenessInvalid, byPath[filepath.Join(tempDir, "malformed.json")].LivenessStatus)
		require.Equal(t, CodexRegistrationLivenessInvalid, byPath[filepath.Join(tempDir, "missing-required.json")].LivenessStatus)
		require.Equal(t, CodexRegistrationLivenessAlive, byPath[filepath.Join(tempDir, "refresh-auth-failure.json")].LivenessStatus)
		require.Contains(t, byPath[filepath.Join(tempDir, "refresh-auth-failure.json")].StatusReason, "existing access_token")
		require.Equal(t, CodexRegistrationLivenessDead, byPath[filepath.Join(tempDir, "probe-401.json")].LivenessStatus)
		require.Equal(t, CodexRegistrationLivenessAlive, byPath[filepath.Join(tempDir, "probe-429.json")].LivenessStatus)
		require.Equal(t, CodexRegistrationLivenessAlive, byPath[filepath.Join(tempDir, "alive.json")].LivenessStatus)
		require.Equal(t, CodexRegistrationLivenessError, byPath[filepath.Join(tempDir, "timeout.json")].LivenessStatus)
		require.Equal(t, CodexRegistrationLivenessError, byPath[filepath.Join(tempDir, "upstream-5xx.json")].LivenessStatus)
		require.Equal(t, CodexRegistrationLivenessError, byPath[filepath.Join(tempDir, "malformed-probe.json")].LivenessStatus)
		require.Equal(t, CodexRegistrationWorkflowDuplicate, byPath[filepath.Join(tempDir, "duplicate-existing.json")].WorkflowState)
		require.Equal(t, CodexRegistrationWorkflowDuplicate, byPath[filepath.Join(tempDir, "duplicate-refreshed-identity.json")].WorkflowState)
		require.Equal(t, "existing@example.com", byPath[filepath.Join(tempDir, "duplicate-refreshed-identity.json")].Email)
		require.Equal(t, "acct-existing", byPath[filepath.Join(tempDir, "duplicate-refreshed-identity.json")].AccountID)
		_, staleDetectedExists := repo.getByID(staleDetected.ID)
		require.False(t, staleDetectedExists)
		staleImportedState, staleImportedExists := repo.getByID(staleImported.ID)
		require.True(t, staleImportedExists)
		require.Equal(t, CodexRegistrationWorkflowImported, staleImportedState.WorkflowState)

		_, hasIgnoredTxt := byPath[filepath.Join(tempDir, "ignored.txt")]
		require.False(t, hasIgnoredTxt)
		_, hasIgnoredNonCodex := byPath[filepath.Join(tempDir, "ignored-non-codex.json")]
		require.False(t, hasIgnoredNonCodex)
	})

	t.Run("scan defaults to gpt-5.4-mini and passes through explicit model", func(t *testing.T) {
		tempDir := t.TempDir()
		require.NoError(t, writeJSONFile(filepath.Join(tempDir, "alive.json"), map[string]any{
			"type":          "codex",
			"refresh_token": "rt-alive",
			"email":         "alive@example.com",
			"account_id":    "acct-alive",
		}))

		repo := newCodexCandidateRepoStub()
		oauth := &codexOAuthRefresherStub{
			outcomes: map[string]codexOAuthOutcome{
				"rt-alive": {
					info: &OpenAITokenInfo{
						AccessToken:      "at-alive",
						Email:            "alive@example.com",
						ChatGPTAccountID: "acct-alive",
					},
				},
			},
		}

		probeBodies := make([]string, 0, 2)
		probeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			probeBodies = append(probeBodies, string(body))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		defer probeServer.Close()

		svc := NewCodexRegistrationService(&config.Config{CodexRegistration: config.CodexRegistrationConfig{
			ScanWorkers:         1,
			ProbeTimeoutSeconds: 1,
		}}, repo, &codexAccountReaderStub{}, oauth, &codexAccountCreatorStub{})
		svc.sourceDir = tempDir
		svc.probeURL = probeServer.URL + "/backend-api/codex/responses/compact"

		_, err := svc.Scan(context.Background(), "")
		require.NoError(t, err)
		require.NotEmpty(t, probeBodies)
		require.Contains(t, probeBodies[0], `"model":"gpt-5.4-mini"`)
		require.NotEmpty(t, oauth.calls)
		require.Equal(t, "", oauth.calls[0].ClientID)

		probeBodies = nil
		oauth.calls = nil

		_, err = svc.Scan(context.Background(), "gpt-5.4")
		require.NoError(t, err)
		require.NotEmpty(t, probeBodies)
		require.Contains(t, probeBodies[0], `"model":"gpt-5.4"`)
	})

	t.Run("select and unselect transitions", func(t *testing.T) {
		repo := newCodexCandidateRepoStub()
		now := time.Now().UTC()
		repo.mustInsert(CodexRegistrationCandidateState{
			SourcePath:        "/tmp/alive-detected.json",
			SourceFilename:    "alive-detected.json",
			SourceMtime:       now,
			SourceFingerprint: "fp-1",
			LivenessStatus:    CodexRegistrationLivenessAlive,
			WorkflowState:     CodexRegistrationWorkflowDetected,
			LastCheckedAt:     now,
			CreatedAt:         now,
			UpdatedAt:         now,
		})
		repo.mustInsert(CodexRegistrationCandidateState{
			SourcePath:        "/tmp/dead-detected.json",
			SourceFilename:    "dead-detected.json",
			SourceMtime:       now,
			SourceFingerprint: "fp-2",
			LivenessStatus:    CodexRegistrationLivenessDead,
			WorkflowState:     CodexRegistrationWorkflowDetected,
			LastCheckedAt:     now,
			CreatedAt:         now,
			UpdatedAt:         now,
		})
		staged := repo.mustInsert(CodexRegistrationCandidateState{
			SourcePath:        "/tmp/staged.json",
			SourceFilename:    "staged.json",
			SourceMtime:       now,
			SourceFingerprint: "fp-3",
			LivenessStatus:    CodexRegistrationLivenessAlive,
			WorkflowState:     CodexRegistrationWorkflowStaged,
			LastCheckedAt:     now,
			CreatedAt:         now,
			UpdatedAt:         now,
		})
		duplicate := repo.mustInsert(CodexRegistrationCandidateState{
			SourcePath:        "/tmp/duplicate.json",
			SourceFilename:    "duplicate.json",
			SourceMtime:       now,
			SourceFingerprint: "fp-4",
			LivenessStatus:    CodexRegistrationLivenessAlive,
			WorkflowState:     CodexRegistrationWorkflowDuplicate,
			LastCheckedAt:     now,
			CreatedAt:         now,
			UpdatedAt:         now,
		})

		svc := NewCodexRegistrationService(&config.Config{CodexRegistration: config.CodexRegistrationConfig{
			ScanWorkers:         2,
			ProbeTimeoutSeconds: 1,
		}}, repo, &codexAccountReaderStub{}, &codexOAuthRefresherStub{}, &codexAccountCreatorStub{})

		all := repo.listStates()
		sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
		aliveDetectedID := all[0].ID
		deadDetectedID := all[1].ID

		require.NoError(t, svc.Stage(context.Background(), []int64{aliveDetectedID}))
		updatedAlive, ok := repo.getByID(aliveDetectedID)
		require.True(t, ok)
		require.Equal(t, CodexRegistrationWorkflowStaged, updatedAlive.WorkflowState)

		err := svc.Stage(context.Background(), []int64{deadDetectedID})
		require.Error(t, err)

		err = svc.Stage(context.Background(), []int64{duplicate.ID})
		require.Error(t, err)

		require.NoError(t, svc.Unstage(context.Background(), []int64{staged.ID}))
		updatedStaged, ok := repo.getByID(staged.ID)
		require.True(t, ok)
		require.Equal(t, CodexRegistrationWorkflowDetected, updatedStaged.WorkflowState)

		err = svc.Unstage(context.Background(), []int64{deadDetectedID})
		require.Error(t, err)
	})

	t.Run("stage and unstage avoid partial application", func(t *testing.T) {
		repo := newCodexCandidateRepoStub()
		now := time.Now().UTC()
		valid := repo.mustInsert(CodexRegistrationCandidateState{
			SourcePath:        "/tmp/valid-detected.json",
			SourceFilename:    "valid-detected.json",
			SourceMtime:       now,
			SourceFingerprint: "fp-valid",
			LivenessStatus:    CodexRegistrationLivenessAlive,
			WorkflowState:     CodexRegistrationWorkflowDetected,
			LastCheckedAt:     now,
			CreatedAt:         now,
			UpdatedAt:         now,
		})
		invalid := repo.mustInsert(CodexRegistrationCandidateState{
			SourcePath:        "/tmp/invalid-detected.json",
			SourceFilename:    "invalid-detected.json",
			SourceMtime:       now,
			SourceFingerprint: "fp-invalid",
			LivenessStatus:    CodexRegistrationLivenessDead,
			WorkflowState:     CodexRegistrationWorkflowDetected,
			LastCheckedAt:     now,
			CreatedAt:         now,
			UpdatedAt:         now,
		})
		staged := repo.mustInsert(CodexRegistrationCandidateState{
			SourcePath:        "/tmp/already-staged.json",
			SourceFilename:    "already-staged.json",
			SourceMtime:       now,
			SourceFingerprint: "fp-staged",
			LivenessStatus:    CodexRegistrationLivenessAlive,
			WorkflowState:     CodexRegistrationWorkflowStaged,
			LastCheckedAt:     now,
			CreatedAt:         now,
			UpdatedAt:         now,
		})

		svc := NewCodexRegistrationService(&config.Config{CodexRegistration: config.CodexRegistrationConfig{
			ScanWorkers:         2,
			ProbeTimeoutSeconds: 1,
		}}, repo, &codexAccountReaderStub{}, &codexOAuthRefresherStub{}, &codexAccountCreatorStub{})

		err := svc.Stage(context.Background(), []int64{valid.ID, invalid.ID})
		require.Error(t, err)
		validAfter, ok := repo.getByID(valid.ID)
		require.True(t, ok)
		require.Equal(t, CodexRegistrationWorkflowDetected, validAfter.WorkflowState)

		err = svc.Stage(context.Background(), []int64{valid.ID, valid.ID})
		require.NoError(t, err)
		validAfter, ok = repo.getByID(valid.ID)
		require.True(t, ok)
		require.Equal(t, CodexRegistrationWorkflowStaged, validAfter.WorkflowState)

		err = svc.Unstage(context.Background(), []int64{staged.ID, invalid.ID})
		require.Error(t, err)
		stagedAfter, ok := repo.getByID(staged.ID)
		require.True(t, ok)
		require.Equal(t, CodexRegistrationWorkflowStaged, stagedAfter.WorkflowState)
	})

	t.Run("import validates group cardinality", func(t *testing.T) {
		repo := newCodexCandidateRepoStub()
		now := time.Now().UTC()
		candidate := repo.mustInsert(CodexRegistrationCandidateState{
			SourcePath:        "/tmp/staged-for-import.json",
			SourceFilename:    "staged-for-import.json",
			SourceMtime:       now,
			SourceFingerprint: "fp-import",
			Email:             "import@example.com",
			AccountID:         "acct-import",
			Type:              "codex",
			LivenessStatus:    CodexRegistrationLivenessAlive,
			WorkflowState:     CodexRegistrationWorkflowStaged,
			LastCheckedAt:     now,
			CreatedAt:         now,
			UpdatedAt:         now,
		})

		svc := NewCodexRegistrationService(&config.Config{CodexRegistration: config.CodexRegistrationConfig{
			ScanWorkers:         1,
			ProbeTimeoutSeconds: 1,
		}}, repo, &codexAccountReaderStub{}, &codexOAuthRefresherStub{}, &codexAccountCreatorStub{})

		_, err := svc.Import(context.Background(), CodexRegistrationImportInput{
			CandidateIDs: []int64{candidate.ID},
			GroupIDs:     nil,
		})
		require.Error(t, err)

		_, err = svc.Import(context.Background(), CodexRegistrationImportInput{
			CandidateIDs: []int64{candidate.ID},
			GroupIDs:     []int64{1, 2},
		})
		require.NoError(t, err)
	})

	t.Run("import rechecks duplicate before account creation", func(t *testing.T) {
		tempDir := t.TempDir()
		sourcePath := filepath.Join(tempDir, "staged.json")
		require.NoError(t, writeJSONFile(sourcePath, map[string]any{
			"type":          "codex",
			"refresh_token": "rt-staged",
			"email":         "staged@example.com",
			"account_id":    "acct-staged",
			"client_id":     "client-from-staged-file",
		}))

		probeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/backend-api/codex/responses/compact":
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"ok":true}`))
			case "/v1/models":
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"data":[{"id":"gpt-5.4"},{"id":"gpt-5.4-mini"}]}`))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer probeServer.Close()

		repo := newCodexCandidateRepoStub()
		accountReader := &codexAccountReaderStub{}
		oauth := &codexOAuthRefresherStub{
			outcomes: map[string]codexOAuthOutcome{
				"rt-staged": {
					info: &OpenAITokenInfo{
						AccessToken:      "at-staged",
						RefreshToken:     "rt-staged-new",
						ExpiresAt:        time.Now().Add(time.Hour).Unix(),
						Email:            "staged@example.com",
						ChatGPTAccountID: "acct-staged",
						ClientID:         "client-from-staged-file",
					},
				},
				"rt-staged-new": {
					info: &OpenAITokenInfo{
						AccessToken:      "at-staged-new",
						RefreshToken:     "rt-staged-new-2",
						ExpiresAt:        time.Now().Add(time.Hour).Unix(),
						Email:            "staged@example.com",
						ChatGPTAccountID: "acct-staged",
						ClientID:         "client-from-staged-file",
					},
				},
			},
		}
		creator := &codexAccountCreatorStub{}

		cfg := &config.Config{CodexRegistration: config.CodexRegistrationConfig{
			ScanWorkers:         2,
			ProbeTimeoutSeconds: 1,
		}}
		svc := NewCodexRegistrationService(cfg, repo, accountReader, oauth, creator)
		svc.sourceDir = tempDir
		svc.probeURL = probeServer.URL + "/backend-api/codex/responses/compact"

		scanned, err := svc.Scan(context.Background(), "")
		require.NoError(t, err)
		require.Len(t, scanned, 1)
		require.Equal(t, CodexRegistrationWorkflowDetected, scanned[0].WorkflowState)
		require.Equal(t, CodexRegistrationLivenessAlive, scanned[0].LivenessStatus)

		require.NoError(t, svc.Stage(context.Background(), []int64{scanned[0].ID}))

		accountReader.accounts = []Account{
			{
				ID:       101,
				Platform: PlatformOpenAI,
				Type:     AccountTypeOAuth,
				Credentials: map[string]any{
					"chatgpt_account_id": "acct-staged",
				},
			},
		}

		result, err := svc.Import(context.Background(), CodexRegistrationImportInput{
			CandidateIDs: []int64{scanned[0].ID},
			GroupIDs:     []int64{123},
			Notes:        "import-notes",
		})
		require.NoError(t, err)
		require.Empty(t, result.ImportedIDs)
		require.Contains(t, result.Failed, scanned[0].ID)
		require.Contains(t, strings.ToLower(result.Failed[scanned[0].ID]), "duplicate")
		require.Empty(t, creator.createdInputs)

		updated, ok := repo.getByID(scanned[0].ID)
		require.True(t, ok)
		require.Equal(t, CodexRegistrationWorkflowDuplicate, updated.WorkflowState)
	})

	t.Run("import supports explicit candidate subset and source client_id", func(t *testing.T) {
		tempDir := t.TempDir()
		selectedPath := filepath.Join(tempDir, "selected.json")
		unselectedPath := filepath.Join(tempDir, "unselected.json")
		require.NoError(t, writeJSONFile(selectedPath, map[string]any{
			"type":          "codex",
			"refresh_token": "rt-selected",
			"email":         "selected@example.com",
			"account_id":    "acct-selected",
			"client_id":     "client-selected",
		}))
		require.NoError(t, writeJSONFile(unselectedPath, map[string]any{
			"type":          "codex",
			"refresh_token": "rt-unselected",
			"email":         "unselected@example.com",
			"account_id":    "acct-unselected",
			"client_id":     "client-unselected",
		}))

		probeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/backend-api/codex/responses/compact":
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"ok":true}`))
			case "/v1/models":
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"data":[{"id":"gpt-5.4"},{"id":"gpt-5.4-mini"}]}`))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer probeServer.Close()

		now := time.Now().UTC()
		selectedFingerprint := mustSourceFingerprint(t, selectedPath)
		selectedMtime := mustSourceMtime(t, selectedPath)
		unselectedFingerprint := mustSourceFingerprint(t, unselectedPath)
		unselectedMtime := mustSourceMtime(t, unselectedPath)
		repo := newCodexCandidateRepoStub()
		selected := repo.mustInsert(CodexRegistrationCandidateState{
			SourcePath:        selectedPath,
			SourceFilename:    filepath.Base(selectedPath),
			SourceMtime:       selectedMtime,
			SourceFingerprint: selectedFingerprint,
			Email:             "selected@example.com",
			AccountID:         "acct-selected",
			Type:              "codex",
			LivenessStatus:    CodexRegistrationLivenessAlive,
			WorkflowState:     CodexRegistrationWorkflowStaged,
			LastCheckedAt:     now,
			CreatedAt:         now,
			UpdatedAt:         now,
		})
		unselected := repo.mustInsert(CodexRegistrationCandidateState{
			SourcePath:        unselectedPath,
			SourceFilename:    filepath.Base(unselectedPath),
			SourceMtime:       unselectedMtime,
			SourceFingerprint: unselectedFingerprint,
			Email:             "unselected@example.com",
			AccountID:         "acct-unselected",
			Type:              "codex",
			LivenessStatus:    CodexRegistrationLivenessAlive,
			WorkflowState:     CodexRegistrationWorkflowStaged,
			LastCheckedAt:     now,
			CreatedAt:         now,
			UpdatedAt:         now,
		})

		oauth := &codexOAuthRefresherStub{
			outcomes: map[string]codexOAuthOutcome{
				"rt-selected": {
					info: &OpenAITokenInfo{
						AccessToken:      "at-selected",
						RefreshToken:     "rt-selected-new",
						ExpiresAt:        time.Now().Add(time.Hour).Unix(),
						Email:            "selected@example.com",
						ChatGPTAccountID: "acct-selected",
					},
				},
				"rt-unselected": {
					info: &OpenAITokenInfo{
						AccessToken:      "at-unselected",
						RefreshToken:     "rt-unselected-new",
						ExpiresAt:        time.Now().Add(time.Hour).Unix(),
						Email:            "unselected@example.com",
						ChatGPTAccountID: "acct-unselected",
					},
				},
			},
		}
		creator := &codexAccountCreatorStub{}

		svc := NewCodexRegistrationService(&config.Config{CodexRegistration: config.CodexRegistrationConfig{
			ScanWorkers:         2,
			ProbeTimeoutSeconds: 1,
		}}, repo, nil, oauth, creator)
		svc.sourceDir = tempDir
		svc.probeURL = probeServer.URL + "/backend-api/codex/responses/compact"

		result, err := svc.Import(context.Background(), CodexRegistrationImportInput{
			CandidateIDs:   []int64{selected.ID},
			GroupIDs:       []int64{321},
			Notes:          "subset-import",
			Concurrency:    testIntPtr(2),
			LoadFactor:     testIntPtr(3),
			Priority:       testIntPtr(4),
			RateMultiplier: testFloat64Ptr(0.5),
		})
		require.NoError(t, err)
		require.Equal(t, []int64{selected.ID}, result.ImportedIDs)
		require.Empty(t, result.Failed)
		require.Len(t, oauth.calls, 1)
		require.Equal(t, "rt-selected", oauth.calls[0].RefreshToken)
		require.Equal(t, "client-selected", oauth.calls[0].ClientID)
		require.Len(t, creator.createdInputs, 1)
		require.True(t, creator.createdInputs[0].SkipDefaultGroupBind)
		require.Equal(t, []int64{321}, creator.createdInputs[0].GroupIDs)
		require.Equal(t, 2, creator.createdInputs[0].Concurrency)
		require.NotNil(t, creator.createdInputs[0].LoadFactor)
		require.Equal(t, 3, *creator.createdInputs[0].LoadFactor)
		require.Equal(t, 4, creator.createdInputs[0].Priority)
		require.NotNil(t, creator.createdInputs[0].RateMultiplier)
		require.Equal(t, 0.5, *creator.createdInputs[0].RateMultiplier)
		require.Equal(t, "client-selected", creator.createdInputs[0].Credentials["client_id"])

		selectedUpdated, ok := repo.getByID(selected.ID)
		require.True(t, ok)
		require.Equal(t, CodexRegistrationWorkflowImported, selectedUpdated.WorkflowState)
		require.NotNil(t, selectedUpdated.ImportedAccountID)
		require.NotNil(t, selectedUpdated.ImportedAt)

		unselectedUpdated, ok := repo.getByID(unselected.ID)
		require.True(t, ok)
		require.Equal(t, CodexRegistrationWorkflowStaged, unselectedUpdated.WorkflowState)
	})

	t.Run("import models for oauth uses built-in models without hitting upstream models endpoint", func(t *testing.T) {
		tempDir := t.TempDir()
		sourcePath := filepath.Join(tempDir, "selected-import-models.json")
		require.NoError(t, writeJSONFile(sourcePath, map[string]any{
			"type":          "codex",
			"refresh_token": "rt-selected-import-models",
			"email":         "selected-import-models@example.com",
			"account_id":    "acct-selected-import-models",
			"client_id":     "client-selected-import-models",
		}))

		upstreamModelHits := 0
		probeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/backend-api/codex/responses/compact":
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"ok":true}`))
			case "/v1/models":
				upstreamModelHits++
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer probeServer.Close()

		now := time.Now().UTC()
		repo := newCodexCandidateRepoStub()
		selected := repo.mustInsert(CodexRegistrationCandidateState{
			SourcePath:        sourcePath,
			SourceFilename:    filepath.Base(sourcePath),
			SourceMtime:       mustSourceMtime(t, sourcePath),
			SourceFingerprint: mustSourceFingerprint(t, sourcePath),
			Email:             "selected-import-models@example.com",
			AccountID:         "acct-selected-import-models",
			Type:              "codex",
			LivenessStatus:    CodexRegistrationLivenessAlive,
			WorkflowState:     CodexRegistrationWorkflowStaged,
			LastCheckedAt:     now,
			CreatedAt:         now,
			UpdatedAt:         now,
		})

		oauth := &codexOAuthRefresherStub{
			outcomes: map[string]codexOAuthOutcome{
				"rt-selected-import-models": {
					info: &OpenAITokenInfo{
						AccessToken:      "at-selected-import-models",
						RefreshToken:     "rt-selected-import-models-new",
						ExpiresAt:        time.Now().Add(time.Hour).Unix(),
						Email:            "selected-import-models@example.com",
						ChatGPTAccountID: "acct-selected-import-models",
						ClientID:         "client-selected-import-models",
					},
				},
			},
		}
		creator := &codexAccountCreatorStub{}

		svc := NewCodexRegistrationService(&config.Config{CodexRegistration: config.CodexRegistrationConfig{
			ScanWorkers:         1,
			ProbeTimeoutSeconds: 1,
		}}, repo, nil, oauth, creator)
		svc.sourceDir = tempDir
		svc.probeURL = probeServer.URL + "/backend-api/codex/responses/compact"

		result, err := svc.Import(context.Background(), CodexRegistrationImportInput{
			CandidateIDs: []int64{selected.ID},
			GroupIDs:     []int64{321},
			ImportModels: true,
		})
		require.NoError(t, err)
		require.Equal(t, []int64{selected.ID}, result.ImportedIDs)
		require.Empty(t, result.Failed)
		require.Len(t, creator.createdInputs, 1)
		require.Equal(t, "client-selected-import-models", creator.createdInputs[0].Credentials["client_id"])

		modelMapping, ok := creator.createdInputs[0].Credentials["model_mapping"].(map[string]string)
		require.True(t, ok)
		require.Contains(t, modelMapping, "gpt-5.4")
		require.Contains(t, modelMapping, "gpt-5.3-codex")
		require.Zero(t, upstreamModelHits)
	})

	t.Run("import from detection selection creates only alive candidates", func(t *testing.T) {
		tempDir := t.TempDir()
		alivePath := filepath.Join(tempDir, "alive-detected.json")
		deadPath := filepath.Join(tempDir, "dead-detected.json")
		require.NoError(t, writeJSONFile(alivePath, map[string]any{
			"type":          "codex",
			"refresh_token": "rt-detected-alive",
			"email":         "detected-alive@example.com",
			"account_id":    "acct-detected-alive",
		}))
		require.NoError(t, writeJSONFile(deadPath, map[string]any{
			"type":          "codex",
			"refresh_token": "rt-detected-dead",
			"email":         "detected-dead@example.com",
			"account_id":    "acct-detected-dead",
		}))

		probeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		defer probeServer.Close()

		now := time.Now().UTC()
		repo := newCodexCandidateRepoStub()
		aliveCandidate := repo.mustInsert(CodexRegistrationCandidateState{
			SourcePath:        alivePath,
			SourceFilename:    filepath.Base(alivePath),
			SourceMtime:       mustSourceMtime(t, alivePath),
			SourceFingerprint: mustSourceFingerprint(t, alivePath),
			Email:             "detected-alive@example.com",
			AccountID:         "acct-detected-alive",
			Type:              "codex",
			LivenessStatus:    CodexRegistrationLivenessAlive,
			WorkflowState:     CodexRegistrationWorkflowDetected,
			LastCheckedAt:     now,
			CreatedAt:         now,
			UpdatedAt:         now,
		})
		deadCandidate := repo.mustInsert(CodexRegistrationCandidateState{
			SourcePath:        deadPath,
			SourceFilename:    filepath.Base(deadPath),
			SourceMtime:       mustSourceMtime(t, deadPath),
			SourceFingerprint: mustSourceFingerprint(t, deadPath),
			Email:             "detected-dead@example.com",
			AccountID:         "acct-detected-dead",
			Type:              "codex",
			LivenessStatus:    CodexRegistrationLivenessDead,
			WorkflowState:     CodexRegistrationWorkflowDetected,
			StatusReason:      "refresh auth failure",
			LastCheckedAt:     now,
			CreatedAt:         now,
			UpdatedAt:         now,
		})

		oauth := &codexOAuthRefresherStub{
			outcomes: map[string]codexOAuthOutcome{
				"rt-detected-alive": {
					info: &OpenAITokenInfo{
						AccessToken:      "at-detected-alive",
						RefreshToken:     "rt-detected-alive-new",
						ExpiresAt:        time.Now().Add(time.Hour).Unix(),
						Email:            "detected-alive@example.com",
						ChatGPTAccountID: "acct-detected-alive",
					},
				},
			},
		}
		creator := &codexAccountCreatorStub{}

		svc := NewCodexRegistrationService(&config.Config{CodexRegistration: config.CodexRegistrationConfig{
			ScanWorkers:         1,
			ProbeTimeoutSeconds: 1,
		}}, repo, &codexAccountReaderStub{}, oauth, creator)
		svc.sourceDir = tempDir
		svc.probeURL = probeServer.URL + "/backend-api/codex/responses/compact"

		result, err := svc.Import(context.Background(), CodexRegistrationImportInput{
			CandidateIDs: []int64{aliveCandidate.ID, deadCandidate.ID},
			GroupIDs:     []int64{2, 3},
		})
		require.NoError(t, err)
		require.Equal(t, []int64{aliveCandidate.ID}, result.ImportedIDs)
		require.Contains(t, result.Failed, deadCandidate.ID)
		require.Contains(t, result.Failed[deadCandidate.ID], "refresh auth failure")
		require.Len(t, creator.createdInputs, 1)
		require.Equal(t, []int64{2, 3}, creator.createdInputs[0].GroupIDs)
	})

	t.Run("import allows reimport when historical workflow is stale but current accounts do not conflict", func(t *testing.T) {
		tempDir := t.TempDir()
		importedPath := filepath.Join(tempDir, "stale-imported.json")
		duplicatePath := filepath.Join(tempDir, "stale-duplicate.json")
		require.NoError(t, writeJSONFile(importedPath, map[string]any{
			"type":          "codex",
			"refresh_token": "rt-stale-imported",
			"email":         "stale-imported@example.com",
			"account_id":    "acct-stale-imported",
		}))
		require.NoError(t, writeJSONFile(duplicatePath, map[string]any{
			"type":          "codex",
			"refresh_token": "rt-stale-duplicate",
			"email":         "stale-duplicate@example.com",
			"account_id":    "acct-stale-duplicate",
		}))

		probeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		defer probeServer.Close()

		now := time.Now().UTC()
		repo := newCodexCandidateRepoStub()
		importedCandidate := repo.mustInsert(CodexRegistrationCandidateState{
			SourcePath:        importedPath,
			SourceFilename:    filepath.Base(importedPath),
			SourceMtime:       mustSourceMtime(t, importedPath),
			SourceFingerprint: mustSourceFingerprint(t, importedPath),
			Email:             "stale-imported@example.com",
			AccountID:         "acct-stale-imported",
			Type:              "codex",
			LivenessStatus:    CodexRegistrationLivenessAlive,
			WorkflowState:     CodexRegistrationWorkflowImported,
			LastCheckedAt:     now,
			CreatedAt:         now,
			UpdatedAt:         now,
		})
		duplicateCandidate := repo.mustInsert(CodexRegistrationCandidateState{
			SourcePath:        duplicatePath,
			SourceFilename:    filepath.Base(duplicatePath),
			SourceMtime:       mustSourceMtime(t, duplicatePath),
			SourceFingerprint: mustSourceFingerprint(t, duplicatePath),
			Email:             "stale-duplicate@example.com",
			AccountID:         "acct-stale-duplicate",
			Type:              "codex",
			LivenessStatus:    CodexRegistrationLivenessAlive,
			WorkflowState:     CodexRegistrationWorkflowDuplicate,
			LastCheckedAt:     now,
			CreatedAt:         now,
			UpdatedAt:         now,
		})

		oauth := &codexOAuthRefresherStub{
			outcomes: map[string]codexOAuthOutcome{
				"rt-stale-imported": {
					info: &OpenAITokenInfo{
						AccessToken:      "at-stale-imported",
						RefreshToken:     "rt-stale-imported-new",
						ExpiresAt:        time.Now().Add(time.Hour).Unix(),
						Email:            "stale-imported@example.com",
						ChatGPTAccountID: "acct-stale-imported",
					},
				},
				"rt-stale-duplicate": {
					info: &OpenAITokenInfo{
						AccessToken:      "at-stale-duplicate",
						RefreshToken:     "rt-stale-duplicate-new",
						ExpiresAt:        time.Now().Add(time.Hour).Unix(),
						Email:            "stale-duplicate@example.com",
						ChatGPTAccountID: "acct-stale-duplicate",
					},
				},
			},
		}
		creator := &codexAccountCreatorStub{}

		svc := NewCodexRegistrationService(&config.Config{CodexRegistration: config.CodexRegistrationConfig{
			ScanWorkers:         1,
			ProbeTimeoutSeconds: 1,
		}}, repo, &codexAccountReaderStub{}, oauth, creator)
		svc.sourceDir = tempDir
		svc.probeURL = probeServer.URL + "/backend-api/codex/responses/compact"

		result, err := svc.Import(context.Background(), CodexRegistrationImportInput{
			CandidateIDs: []int64{importedCandidate.ID, duplicateCandidate.ID},
			GroupIDs:     []int64{2},
		})
		require.NoError(t, err)
		require.ElementsMatch(t, []int64{importedCandidate.ID, duplicateCandidate.ID}, result.ImportedIDs)
		require.Empty(t, result.Failed)
		require.Len(t, creator.createdInputs, 2)
	})

	t.Run("import falls back to existing access token when refresh auth fails", func(t *testing.T) {
		tempDir := t.TempDir()
		sourcePath := filepath.Join(tempDir, "fallback-import.json")
		require.NoError(t, writeJSONFile(sourcePath, map[string]any{
			"type":          "codex",
			"refresh_token": "rt-import-auth-fail",
			"access_token":  "at-import-fallback-alive",
			"id_token":      "id-import-fallback",
			"email":         "fallback-import@example.com",
			"account_id":    "acct-import-fallback",
			"client_id":     "client-import-fallback",
			"expires_at":    time.Now().Add(20 * time.Minute).UTC().Format(time.RFC3339),
		}))

		probeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			switch token {
			case "at-import-fallback-alive":
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":{"type":"usage_limit_reached","message":"limit reached"}}`))
			default:
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			}
		}))
		defer probeServer.Close()

		repo := newCodexCandidateRepoStub()
		oauth := &codexOAuthRefresherStub{
			outcomes: map[string]codexOAuthOutcome{
				"rt-import-auth-fail": {
					err: infraerrors.New(http.StatusUnauthorized, "OPENAI_OAUTH_TOKEN_REFRESH_FAILED", "invalid_grant"),
				},
			},
		}
		creator := &codexAccountCreatorStub{}

		svc := NewCodexRegistrationService(&config.Config{CodexRegistration: config.CodexRegistrationConfig{
			ScanWorkers:         1,
			ProbeTimeoutSeconds: 1,
		}}, repo, &codexAccountReaderStub{}, oauth, creator)
		svc.sourceDir = tempDir
		svc.probeURL = probeServer.URL + "/backend-api/codex/responses/compact"

		scanned, err := svc.Scan(context.Background(), "")
		require.NoError(t, err)
		require.Len(t, scanned, 1)
		require.Equal(t, CodexRegistrationLivenessAlive, scanned[0].LivenessStatus)

		require.NoError(t, svc.Stage(context.Background(), []int64{scanned[0].ID}))

		result, err := svc.Import(context.Background(), CodexRegistrationImportInput{
			CandidateIDs: []int64{scanned[0].ID},
			GroupIDs:     []int64{2, 3},
		})
		require.NoError(t, err)
		require.Equal(t, []int64{scanned[0].ID}, result.ImportedIDs)
		require.Empty(t, result.Failed)
		require.Len(t, creator.createdInputs, 1)
		require.Equal(t, "at-import-fallback-alive", creator.createdInputs[0].Credentials["access_token"])
		_, hasRefreshToken := creator.createdInputs[0].Credentials["refresh_token"]
		require.False(t, hasRefreshToken)
	})

	t.Run("scan then import reuses refreshed token snapshot", func(t *testing.T) {
		tempDir := t.TempDir()
		sourcePath := filepath.Join(tempDir, "snapshot-import.json")
		require.NoError(t, writeJSONFile(sourcePath, map[string]any{
			"type":          "codex",
			"refresh_token": "rt-original",
			"email":         "snapshot@example.com",
			"account_id":    "acct-snapshot",
		}))

		probeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		defer probeServer.Close()

		repo := newCodexCandidateRepoStub()
		oauth := &codexOAuthRefresherStub{
			outcomes: map[string]codexOAuthOutcome{
				"rt-original": {
					info: &OpenAITokenInfo{
						AccessToken:      "at-original",
						RefreshToken:     "rt-rotated",
						ExpiresAt:        time.Now().Add(time.Hour).Unix(),
						Email:            "snapshot@example.com",
						ChatGPTAccountID: "acct-snapshot",
					},
				},
				"rt-rotated": {
					info: &OpenAITokenInfo{
						AccessToken:      "at-rotated",
						RefreshToken:     "rt-rotated-2",
						ExpiresAt:        time.Now().Add(time.Hour).Unix(),
						Email:            "snapshot@example.com",
						ChatGPTAccountID: "acct-snapshot",
					},
				},
			},
		}
		creator := &codexAccountCreatorStub{}

		svc := NewCodexRegistrationService(&config.Config{CodexRegistration: config.CodexRegistrationConfig{
			ScanWorkers:         1,
			ProbeTimeoutSeconds: 1,
		}}, repo, &codexAccountReaderStub{}, oauth, creator)
		svc.sourceDir = tempDir
		svc.probeURL = probeServer.URL + "/backend-api/codex/responses/compact"

		scanned, err := svc.Scan(context.Background(), "")
		require.NoError(t, err)
		require.Len(t, scanned, 1)
		require.Equal(t, CodexRegistrationLivenessAlive, scanned[0].LivenessStatus)
		require.Len(t, oauth.calls, 1)
		require.Equal(t, "rt-original", oauth.calls[0].RefreshToken)

		require.NoError(t, svc.Stage(context.Background(), []int64{scanned[0].ID}))

		result, err := svc.Import(context.Background(), CodexRegistrationImportInput{
			CandidateIDs: []int64{scanned[0].ID},
			GroupIDs:     []int64{2, 3},
		})
		require.NoError(t, err)
		require.Equal(t, []int64{scanned[0].ID}, result.ImportedIDs)
		require.Empty(t, result.Failed)
		require.Len(t, oauth.calls, 2)
		require.Equal(t, "rt-rotated", oauth.calls[1].RefreshToken)
		require.Len(t, creator.createdInputs, 1)
		require.Equal(t, []int64{2, 3}, creator.createdInputs[0].GroupIDs)
	})

	t.Run("scan and import tolerate missing account id when refresh fills it", func(t *testing.T) {
		tempDir := t.TempDir()
		sourcePath := filepath.Join(tempDir, "missing-account-id.json")
		require.NoError(t, writeJSONFile(sourcePath, map[string]any{
			"type":          "codex",
			"refresh_token": "rt-missing-account-id",
			"email":         "filled@example.com",
		}))

		probeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		defer probeServer.Close()

		repo := newCodexCandidateRepoStub()
		oauth := &codexOAuthRefresherStub{
			outcomes: map[string]codexOAuthOutcome{
				"rt-missing-account-id": {
					info: &OpenAITokenInfo{
						AccessToken:      "at-filled",
						RefreshToken:     "rt-filled",
						ExpiresAt:        time.Now().Add(time.Hour).Unix(),
						Email:            "filled@example.com",
						ChatGPTAccountID: "acct-filled",
					},
				},
				"rt-filled": {
					info: &OpenAITokenInfo{
						AccessToken:      "at-filled-2",
						RefreshToken:     "rt-filled-2",
						ExpiresAt:        time.Now().Add(time.Hour).Unix(),
						Email:            "filled@example.com",
						ChatGPTAccountID: "acct-filled",
					},
				},
			},
		}
		creator := &codexAccountCreatorStub{}

		svc := NewCodexRegistrationService(&config.Config{CodexRegistration: config.CodexRegistrationConfig{
			ScanWorkers:         1,
			ProbeTimeoutSeconds: 1,
		}}, repo, &codexAccountReaderStub{}, oauth, creator)
		svc.sourceDir = tempDir
		svc.probeURL = probeServer.URL + "/backend-api/codex/responses/compact"

		scanned, err := svc.Scan(context.Background(), "")
		require.NoError(t, err)
		require.Len(t, scanned, 1)
		require.Equal(t, CodexRegistrationLivenessAlive, scanned[0].LivenessStatus)
		require.Equal(t, "acct-filled", scanned[0].AccountID)

		require.NoError(t, svc.Stage(context.Background(), []int64{scanned[0].ID}))

		result, err := svc.Import(context.Background(), CodexRegistrationImportInput{
			CandidateIDs: []int64{scanned[0].ID},
			GroupIDs:     []int64{11},
		})
		require.NoError(t, err)
		require.Equal(t, []int64{scanned[0].ID}, result.ImportedIDs)
		require.Empty(t, result.Failed)
		require.Len(t, creator.createdInputs, 1)
		require.Equal(t, "acct-filled", creator.createdInputs[0].Credentials["chatgpt_account_id"])
		require.Equal(t, "acct-filled", creator.createdInputs[0].Credentials["account_id"])
	})

	t.Run("import rechecks duplicates immediately before create account", func(t *testing.T) {
		tempDir := t.TempDir()
		sourcePath := filepath.Join(tempDir, "staged-race.json")
		require.NoError(t, writeJSONFile(sourcePath, map[string]any{
			"type":          "codex",
			"refresh_token": "rt-race",
			"email":         "race@example.com",
			"account_id":    "acct-race",
		}))

		probeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		defer probeServer.Close()

		now := time.Now().UTC()
		sourceFingerprint := mustSourceFingerprint(t, sourcePath)
		sourceMtime := mustSourceMtime(t, sourcePath)
		repo := newCodexCandidateRepoStub()
		candidate := repo.mustInsert(CodexRegistrationCandidateState{
			SourcePath:        sourcePath,
			SourceFilename:    filepath.Base(sourcePath),
			SourceMtime:       sourceMtime,
			SourceFingerprint: sourceFingerprint,
			Email:             "race@example.com",
			AccountID:         "acct-race",
			Type:              "codex",
			LivenessStatus:    CodexRegistrationLivenessAlive,
			WorkflowState:     CodexRegistrationWorkflowStaged,
			LastCheckedAt:     now,
			CreatedAt:         now,
			UpdatedAt:         now,
		})

		accountReader := &codexAccountReaderStub{
			accountsByCall: [][]Account{
				{},
				{
					{
						ID:       777,
						Platform: PlatformOpenAI,
						Type:     AccountTypeOAuth,
						Credentials: map[string]any{
							"chatgpt_account_id": "acct-race",
						},
					},
				},
			},
		}
		oauth := &codexOAuthRefresherStub{
			outcomes: map[string]codexOAuthOutcome{
				"rt-race": {
					info: &OpenAITokenInfo{
						AccessToken:      "at-race",
						RefreshToken:     "rt-race-new",
						ExpiresAt:        time.Now().Add(time.Hour).Unix(),
						Email:            "race@example.com",
						ChatGPTAccountID: "acct-race",
					},
				},
			},
		}
		creator := &codexAccountCreatorStub{}

		svc := NewCodexRegistrationService(&config.Config{CodexRegistration: config.CodexRegistrationConfig{
			ScanWorkers:         1,
			ProbeTimeoutSeconds: 1,
		}}, repo, accountReader, oauth, creator)
		svc.sourceDir = tempDir
		svc.probeURL = probeServer.URL + "/backend-api/codex/responses/compact"

		result, err := svc.Import(context.Background(), CodexRegistrationImportInput{
			CandidateIDs: []int64{candidate.ID},
			GroupIDs:     []int64{123},
		})
		require.NoError(t, err)
		require.Empty(t, result.ImportedIDs)
		require.Contains(t, result.Failed, candidate.ID)
		require.Contains(t, strings.ToLower(result.Failed[candidate.ID]), "duplicate")
		require.Empty(t, creator.createdInputs)
		require.GreaterOrEqual(t, accountReader.callCount(), 2)

		updated, ok := repo.getByID(candidate.ID)
		require.True(t, ok)
		require.Equal(t, CodexRegistrationWorkflowDuplicate, updated.WorkflowState)
	})

	t.Run("import rechecks source type and refuses non-codex source", func(t *testing.T) {
		tempDir := t.TempDir()
		sourcePath := filepath.Join(tempDir, "staged-type-changed.json")
		require.NoError(t, writeJSONFile(sourcePath, map[string]any{
			"type":          "other",
			"refresh_token": "rt-other",
			"email":         "other@example.com",
			"account_id":    "acct-other",
		}))

		repo := newCodexCandidateRepoStub()
		now := time.Now().UTC()
		sourceFingerprint := mustSourceFingerprint(t, sourcePath)
		sourceMtime := mustSourceMtime(t, sourcePath)
		candidate := repo.mustInsert(CodexRegistrationCandidateState{
			SourcePath:        sourcePath,
			SourceFilename:    filepath.Base(sourcePath),
			SourceMtime:       sourceMtime,
			SourceFingerprint: sourceFingerprint,
			Email:             "other@example.com",
			AccountID:         "acct-other",
			Type:              "codex",
			LivenessStatus:    CodexRegistrationLivenessAlive,
			WorkflowState:     CodexRegistrationWorkflowStaged,
			LastCheckedAt:     now,
			CreatedAt:         now,
			UpdatedAt:         now,
		})

		oauth := &codexOAuthRefresherStub{
			outcomes: map[string]codexOAuthOutcome{
				"rt-other": {
					info: &OpenAITokenInfo{
						AccessToken:      "at-other",
						RefreshToken:     "rt-other-new",
						ExpiresAt:        time.Now().Add(time.Hour).Unix(),
						Email:            "other@example.com",
						ChatGPTAccountID: "acct-other",
					},
				},
			},
		}
		creator := &codexAccountCreatorStub{}
		svc := NewCodexRegistrationService(&config.Config{CodexRegistration: config.CodexRegistrationConfig{
			ScanWorkers:         1,
			ProbeTimeoutSeconds: 1,
		}}, repo, &codexAccountReaderStub{}, oauth, creator)
		svc.sourceDir = tempDir

		result, err := svc.Import(context.Background(), CodexRegistrationImportInput{
			CandidateIDs: []int64{candidate.ID},
			GroupIDs:     []int64{456},
		})
		require.NoError(t, err)
		require.Empty(t, result.ImportedIDs)
		require.Contains(t, result.Failed, candidate.ID)
		require.Contains(t, strings.ToLower(result.Failed[candidate.ID]), "invalid source type")
		require.Empty(t, creator.createdInputs)
		require.Empty(t, oauth.calls)

		updated, ok := repo.getByID(candidate.ID)
		require.True(t, ok)
		require.Equal(t, CodexRegistrationWorkflowDetected, updated.WorkflowState)
		require.Equal(t, CodexRegistrationLivenessInvalid, updated.LivenessStatus)
	})

	t.Run("import requires unchanged staged fingerprint", func(t *testing.T) {
		tempDir := t.TempDir()
		sourcePath := filepath.Join(tempDir, "staged-changed.json")
		require.NoError(t, writeJSONFile(sourcePath, map[string]any{
			"type":          "codex",
			"refresh_token": "rt-changed",
			"email":         "changed@example.com",
			"account_id":    "acct-changed",
		}))

		repo := newCodexCandidateRepoStub()
		now := time.Now().UTC()
		candidate := repo.mustInsert(CodexRegistrationCandidateState{
			SourcePath:        sourcePath,
			SourceFilename:    filepath.Base(sourcePath),
			SourceMtime:       now,
			SourceFingerprint: "old-fingerprint",
			Email:             "changed@example.com",
			AccountID:         "acct-changed",
			Type:              "codex",
			LivenessStatus:    CodexRegistrationLivenessAlive,
			WorkflowState:     CodexRegistrationWorkflowStaged,
			LastCheckedAt:     now,
			CreatedAt:         now,
			UpdatedAt:         now,
		})

		oauth := &codexOAuthRefresherStub{
			outcomes: map[string]codexOAuthOutcome{
				"rt-changed": {
					info: &OpenAITokenInfo{
						AccessToken:      "at-changed",
						RefreshToken:     "rt-changed-new",
						ExpiresAt:        time.Now().Add(time.Hour).Unix(),
						Email:            "changed@example.com",
						ChatGPTAccountID: "acct-changed",
					},
				},
			},
		}
		creator := &codexAccountCreatorStub{}
		svc := NewCodexRegistrationService(&config.Config{CodexRegistration: config.CodexRegistrationConfig{
			ScanWorkers:         1,
			ProbeTimeoutSeconds: 1,
		}}, repo, &codexAccountReaderStub{}, oauth, creator)
		svc.sourceDir = tempDir

		result, err := svc.Import(context.Background(), CodexRegistrationImportInput{
			CandidateIDs: []int64{candidate.ID},
			GroupIDs:     []int64{789},
		})
		require.NoError(t, err)
		require.Empty(t, result.ImportedIDs)
		require.Contains(t, result.Failed, candidate.ID)
		require.Contains(t, strings.ToLower(result.Failed[candidate.ID]), "rescan required")
		require.Empty(t, creator.createdInputs)
		require.Empty(t, oauth.calls)

		updated, ok := repo.getByID(candidate.ID)
		require.True(t, ok)
		require.Equal(t, CodexRegistrationWorkflowDetected, updated.WorkflowState)
	})

	t.Run("import tolerates benign scan refresh before marking imported", func(t *testing.T) {
		tempDir := t.TempDir()
		sourcePath := filepath.Join(tempDir, "staged-benign-refresh.json")
		require.NoError(t, writeJSONFile(sourcePath, map[string]any{
			"type":          "codex",
			"refresh_token": "rt-benign",
			"email":         "benign@example.com",
			"account_id":    "acct-benign",
		}))

		probeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		defer probeServer.Close()

		repo := newCodexCandidateRepoStub()
		now := time.Now().UTC()
		sourceFingerprint := mustSourceFingerprint(t, sourcePath)
		sourceMtime := mustSourceMtime(t, sourcePath)
		candidate := repo.mustInsert(CodexRegistrationCandidateState{
			SourcePath:        sourcePath,
			SourceFilename:    filepath.Base(sourcePath),
			SourceMtime:       sourceMtime,
			SourceFingerprint: sourceFingerprint,
			Email:             "benign@example.com",
			AccountID:         "acct-benign",
			Type:              "codex",
			LivenessStatus:    CodexRegistrationLivenessAlive,
			WorkflowState:     CodexRegistrationWorkflowStaged,
			LastCheckedAt:     now,
			CreatedAt:         now,
			UpdatedAt:         now,
		})

		oauth := &codexOAuthRefresherStub{
			outcomes: map[string]codexOAuthOutcome{
				"rt-benign": {
					info: &OpenAITokenInfo{
						AccessToken:      "at-benign",
						RefreshToken:     "rt-benign-new",
						ExpiresAt:        time.Now().Add(time.Hour).Unix(),
						Email:            "benign@example.com",
						ChatGPTAccountID: "acct-benign",
					},
				},
			},
		}
		creator := &codexAccountCreatorStub{
			onCreate: func(input *CreateAccountInput) {
				current, ok := repo.getByID(candidate.ID)
				require.True(t, ok)
				current.LastCheckedAt = current.LastCheckedAt.Add(10 * time.Second)
				current.UpdatedAt = current.UpdatedAt.Add(10 * time.Second)
				_, err := repo.Update(context.Background(), current, nil)
				require.NoError(t, err)
			},
		}
		svc := NewCodexRegistrationService(&config.Config{CodexRegistration: config.CodexRegistrationConfig{
			ScanWorkers:         1,
			ProbeTimeoutSeconds: 1,
		}}, repo, &codexAccountReaderStub{}, oauth, creator)
		svc.sourceDir = tempDir
		svc.probeURL = probeServer.URL + "/backend-api/codex/responses/compact"

		result, err := svc.Import(context.Background(), CodexRegistrationImportInput{
			CandidateIDs: []int64{candidate.ID},
			GroupIDs:     []int64{654},
		})
		require.NoError(t, err)
		require.Equal(t, []int64{candidate.ID}, result.ImportedIDs)
		require.Empty(t, result.Failed)

		updated, ok := repo.getByID(candidate.ID)
		require.True(t, ok)
		require.Equal(t, CodexRegistrationWorkflowImported, updated.WorkflowState)
		require.NotNil(t, updated.ImportedAccountID)
		require.NotNil(t, updated.ImportedAt)
	})

	t.Run("import tolerates guarded post-create update conflict", func(t *testing.T) {
		tempDir := t.TempDir()
		sourcePath := filepath.Join(tempDir, "staged-post-create-conflict.json")
		require.NoError(t, writeJSONFile(sourcePath, map[string]any{
			"type":          "codex",
			"refresh_token": "rt-post-create",
			"email":         "post-create@example.com",
			"account_id":    "acct-post-create",
		}))

		probeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		defer probeServer.Close()

		repo := newCodexCandidateRepoStub()
		now := time.Now().UTC()
		sourceFingerprint := mustSourceFingerprint(t, sourcePath)
		sourceMtime := mustSourceMtime(t, sourcePath)
		candidate := repo.mustInsert(CodexRegistrationCandidateState{
			SourcePath:        sourcePath,
			SourceFilename:    filepath.Base(sourcePath),
			SourceMtime:       sourceMtime,
			SourceFingerprint: sourceFingerprint,
			Email:             "post-create@example.com",
			AccountID:         "acct-post-create",
			Type:              "codex",
			LivenessStatus:    CodexRegistrationLivenessAlive,
			WorkflowState:     CodexRegistrationWorkflowStaged,
			LastCheckedAt:     now,
			CreatedAt:         now,
			UpdatedAt:         now,
		})

		oauth := &codexOAuthRefresherStub{
			outcomes: map[string]codexOAuthOutcome{
				"rt-post-create": {
					info: &OpenAITokenInfo{
						AccessToken:      "at-post-create",
						RefreshToken:     "rt-post-create-new",
						ExpiresAt:        time.Now().Add(time.Hour).Unix(),
						Email:            "post-create@example.com",
						ChatGPTAccountID: "acct-post-create",
					},
				},
			},
		}
		creator := &codexAccountCreatorStub{}
		repo.failNextExpectedUpdate = true

		svc := NewCodexRegistrationService(&config.Config{CodexRegistration: config.CodexRegistrationConfig{
			ScanWorkers:         1,
			ProbeTimeoutSeconds: 1,
		}}, repo, &codexAccountReaderStub{}, oauth, creator)
		svc.sourceDir = tempDir
		svc.probeURL = probeServer.URL + "/backend-api/codex/responses/compact"

		result, err := svc.Import(context.Background(), CodexRegistrationImportInput{
			CandidateIDs: []int64{candidate.ID},
			GroupIDs:     []int64{987},
		})
		require.NoError(t, err)
		require.Equal(t, []int64{candidate.ID}, result.ImportedIDs)
		require.Empty(t, result.Failed)

		updated, ok := repo.getByID(candidate.ID)
		require.True(t, ok)
		require.Equal(t, CodexRegistrationWorkflowImported, updated.WorkflowState)
		require.NotNil(t, updated.ImportedAccountID)
		require.NotNil(t, updated.ImportedAt)
	})
}

type codexOAuthOutcome struct {
	info *OpenAITokenInfo
	err  error
}

type codexOAuthRefresherStub struct {
	mu       sync.Mutex
	outcomes map[string]codexOAuthOutcome
	calls    []struct {
		RefreshToken string
		ClientID     string
	}
}

func (s *codexOAuthRefresherStub) RefreshTokenWithClientID(ctx context.Context, refreshToken, proxyURL, clientID string) (*OpenAITokenInfo, error) {
	s.mu.Lock()
	s.calls = append(s.calls, struct {
		RefreshToken string
		ClientID     string
	}{RefreshToken: refreshToken, ClientID: clientID})
	outcome, ok := s.outcomes[refreshToken]
	s.mu.Unlock()

	if !ok {
		return nil, fmt.Errorf("unexpected refresh token: %s", refreshToken)
	}
	if outcome.err != nil {
		return nil, outcome.err
	}
	return outcome.info, nil
}

func (s *codexOAuthRefresherStub) BuildAccountCredentials(tokenInfo *OpenAITokenInfo) map[string]any {
	creds := map[string]any{
		"access_token": tokenInfo.AccessToken,
		"expires_at":   time.Unix(tokenInfo.ExpiresAt, 0).Format(time.RFC3339),
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

type codexAccountReaderStub struct {
	mu             sync.Mutex
	accounts       []Account
	accountsByCall [][]Account
	err            error
	calls          int
}

func (s *codexAccountReaderStub) ListByPlatform(ctx context.Context, platform string) ([]Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	var source []Account
	if len(s.accountsByCall) > 0 {
		callIndex := s.calls - 1
		if callIndex >= len(s.accountsByCall) {
			callIndex = len(s.accountsByCall) - 1
		}
		source = s.accountsByCall[callIndex]
	} else {
		source = s.accounts
	}

	out := make([]Account, 0, len(source))
	for i := range source {
		out = append(out, source[i])
	}
	return out, nil
}

func (s *codexAccountReaderStub) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

type codexAccountCreatorStub struct {
	mu             sync.Mutex
	nextID         int64
	createdInputs  []*CreateAccountInput
	returnErr      error
	returnErrQueue []error
	onCreate       func(*CreateAccountInput)
}

func (s *codexAccountCreatorStub) CreateAccount(ctx context.Context, input *CreateAccountInput) (*Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.returnErrQueue) > 0 {
		err := s.returnErrQueue[0]
		s.returnErrQueue = s.returnErrQueue[1:]
		if err != nil {
			return nil, err
		}
	}
	if s.returnErr != nil {
		return nil, s.returnErr
	}

	copied := *input
	if input.GroupIDs != nil {
		copied.GroupIDs = append([]int64(nil), input.GroupIDs...)
	}
	if input.Credentials != nil {
		copied.Credentials = make(map[string]any, len(input.Credentials))
		for k, v := range input.Credentials {
			copied.Credentials[k] = v
		}
	}
	s.createdInputs = append(s.createdInputs, &copied)
	if s.onCreate != nil {
		s.onCreate(&copied)
	}

	s.nextID++
	if s.nextID == 0 {
		s.nextID = 1
	}
	return &Account{ID: s.nextID}, nil
}

type codexCandidateRepoStub struct {
	mu                     sync.Mutex
	nextID                 int64
	byID                   map[int64]CodexRegistrationCandidateState
	bySource               map[string]int64
	failGetID              map[int64]error
	failUpdate             map[int64]error
	failNextExpectedUpdate bool
}

func newCodexCandidateRepoStub() *codexCandidateRepoStub {
	return &codexCandidateRepoStub{
		nextID:     1,
		byID:       map[int64]CodexRegistrationCandidateState{},
		bySource:   map[string]int64{},
		failGetID:  map[int64]error{},
		failUpdate: map[int64]error{},
	}
}

func (r *codexCandidateRepoStub) List(ctx context.Context) ([]CodexRegistrationCandidateState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]CodexRegistrationCandidateState, 0, len(r.byID))
	for _, candidate := range r.byID {
		out = append(out, candidate)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID == out[j].ID {
			return out[i].SourcePath < out[j].SourcePath
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (r *codexCandidateRepoStub) GetByID(ctx context.Context, id int64) (*CodexRegistrationCandidateState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err, ok := r.failGetID[id]; ok {
		return nil, err
	}
	candidate, ok := r.byID[id]
	if !ok {
		return nil, nil
	}
	copied := candidate
	return &copied, nil
}

func (r *codexCandidateRepoStub) ListByIDs(ctx context.Context, ids []int64) ([]CodexRegistrationCandidateState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]CodexRegistrationCandidateState, 0, len(ids))
	for _, id := range ids {
		candidate, ok := r.byID[id]
		if ok {
			out = append(out, candidate)
		}
	}
	return out, nil
}

func (r *codexCandidateRepoStub) UpsertBySourcePath(ctx context.Context, candidate CodexRegistrationCandidateState) (*CodexRegistrationCandidateState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existingID, ok := r.bySource[candidate.SourcePath]; ok {
		candidate.ID = existingID
	} else {
		candidate.ID = r.nextID
		r.nextID++
	}
	r.byID[candidate.ID] = candidate
	r.bySource[candidate.SourcePath] = candidate.ID
	copied := candidate
	return &copied, nil
}

func (r *codexCandidateRepoStub) Update(ctx context.Context, candidate CodexRegistrationCandidateState, expected *CodexRegistrationCandidateState) (*CodexRegistrationCandidateState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if candidate.ID <= 0 {
		return nil, errors.New("candidate id is required")
	}
	var expectedCandidate CodexRegistrationCandidateState
	if expected != nil {
		expectedCandidate = *expected
	}
	if expected != nil && r.failNextExpectedUpdate {
		r.failNextExpectedUpdate = false
		return nil, fmt.Errorf("candidate %d changed during update", candidate.ID)
	}
	if err, ok := r.failUpdate[candidate.ID]; ok {
		return nil, err
	}
	current, ok := r.byID[candidate.ID]
	if !ok {
		return nil, fmt.Errorf("candidate %d not found", candidate.ID)
	}
	if expected != nil {
		if current.WorkflowState != expectedCandidate.WorkflowState ||
			current.LivenessStatus != expectedCandidate.LivenessStatus ||
			current.SourceFingerprint != expectedCandidate.SourceFingerprint {
			return nil, fmt.Errorf("candidate %d changed during update", candidate.ID)
		}
		if candidate.WorkflowState != CodexRegistrationWorkflowImported &&
			!current.UpdatedAt.UTC().Equal(expectedCandidate.UpdatedAt.UTC()) {
			return nil, fmt.Errorf("candidate %d changed during update", candidate.ID)
		}
	}
	r.byID[candidate.ID] = candidate
	if candidate.SourcePath != "" {
		r.bySource[candidate.SourcePath] = candidate.ID
	}
	copied := candidate
	return &copied, nil
}

func (r *codexCandidateRepoStub) UpdateBatch(ctx context.Context, candidates []CodexRegistrationCandidateState, expected map[int64]CodexRegistrationCandidateState) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(candidates) == 0 {
		return nil
	}
	nextByID := make(map[int64]CodexRegistrationCandidateState, len(r.byID))
	for id, candidate := range r.byID {
		nextByID[id] = candidate
	}
	nextBySource := make(map[string]int64, len(r.bySource))
	for path, id := range r.bySource {
		nextBySource[path] = id
	}

	for _, candidate := range candidates {
		if candidate.ID <= 0 {
			return errors.New("candidate id is required")
		}
		expectedCandidate, ok := expected[candidate.ID]
		if !ok {
			return fmt.Errorf("expected candidate state missing for id %d", candidate.ID)
		}
		if err, ok := r.failUpdate[candidate.ID]; ok {
			return err
		}
		current, ok := nextByID[candidate.ID]
		if !ok {
			return fmt.Errorf("candidate %d not found", candidate.ID)
		}
		if current.WorkflowState != expectedCandidate.WorkflowState ||
			current.LivenessStatus != expectedCandidate.LivenessStatus ||
			current.SourceFingerprint != expectedCandidate.SourceFingerprint ||
			!current.UpdatedAt.UTC().Equal(expectedCandidate.UpdatedAt.UTC()) {
			return fmt.Errorf("candidate %d changed during update", candidate.ID)
		}
		nextByID[candidate.ID] = candidate
		if candidate.SourcePath != "" {
			nextBySource[candidate.SourcePath] = candidate.ID
		}
	}

	r.byID = nextByID
	r.bySource = nextBySource
	return nil
}

func (r *codexCandidateRepoStub) DeleteNonImportedBySourcePathsNotIn(ctx context.Context, sourcePaths []string, checkedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	keep := make(map[string]struct{}, len(sourcePaths))
	for _, path := range sourcePaths {
		keep[path] = struct{}{}
	}

	for id, candidate := range r.byID {
		if candidate.WorkflowState == CodexRegistrationWorkflowImported {
			continue
		}
		if candidate.UpdatedAt.After(checkedAt) {
			continue
		}
		if _, ok := keep[candidate.SourcePath]; ok {
			continue
		}
		delete(r.byID, id)
		delete(r.bySource, candidate.SourcePath)
	}
	return nil
}

func (r *codexCandidateRepoStub) DeleteAll(ctx context.Context) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cleared := len(r.byID)
	r.byID = map[int64]CodexRegistrationCandidateState{}
	r.bySource = map[string]int64{}
	return cleared, nil
}

func (r *codexCandidateRepoStub) mustInsert(candidate CodexRegistrationCandidateState) CodexRegistrationCandidateState {
	inserted, err := r.UpsertBySourcePath(context.Background(), candidate)
	if err != nil {
		panic(err)
	}
	return *inserted
}

func (r *codexCandidateRepoStub) getByID(id int64) (CodexRegistrationCandidateState, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	candidate, ok := r.byID[id]
	return candidate, ok
}

func (r *codexCandidateRepoStub) listStates() []CodexRegistrationCandidateState {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]CodexRegistrationCandidateState, 0, len(r.byID))
	for _, candidate := range r.byID {
		out = append(out, candidate)
	}
	return out
}

func writeJSONFile(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
