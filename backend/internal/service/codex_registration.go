package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

const (
	defaultCodexRegistrationSourceDir     = "/host-cli-proxy-api"
	defaultCodexRegistrationProbeURL      = "https://chatgpt.com/backend-api/codex/responses/compact"
	defaultCodexRegistrationProbeModel    = "gpt-5.4-mini"
	defaultCodexRegistrationScanWorkers   = 4
	defaultCodexRegistrationProbeTimeout  = 8 * time.Second
	codexRegistrationProbeUserAgent       = "codex_cli_rs/0.101.0"
	codexRegistrationProbeRequestMimeType = "application/json"

	codexRegistrationReasonInvalidCandidateSelection = "INVALID_CANDIDATE_SELECTION"
	codexRegistrationReasonCandidateNotFound         = "CANDIDATE_NOT_FOUND"
	codexRegistrationReasonInvalidStageTransition    = "INVALID_STAGE_TRANSITION"
	codexRegistrationReasonInvalidUnstageTransition  = "INVALID_UNSTAGE_TRANSITION"
	codexRegistrationReasonCandidateNotStaged        = "CANDIDATE_NOT_STAGED"
	codexRegistrationReasonInvalidGroupSelection     = "INVALID_GROUP_SELECTION"
)

var codexRegistrationAuthFailureMarkers = []string{
	"unauthorized",
	"invalid_grant",
	"invalid_client",
	"unauthorized_client",
	"token_invalidated",
	"token_revoked",
	"invalidated oauth token",
	"authentication token has been invalidated",
}

type CodexRegistrationLivenessStatus string

const (
	CodexRegistrationLivenessAlive   CodexRegistrationLivenessStatus = "alive"
	CodexRegistrationLivenessDead    CodexRegistrationLivenessStatus = "dead"
	CodexRegistrationLivenessInvalid CodexRegistrationLivenessStatus = "invalid"
	CodexRegistrationLivenessError   CodexRegistrationLivenessStatus = "error"
)

type CodexRegistrationWorkflowState string

const (
	CodexRegistrationWorkflowDetected  CodexRegistrationWorkflowState = "detected"
	CodexRegistrationWorkflowStaged    CodexRegistrationWorkflowState = "staged"
	CodexRegistrationWorkflowDuplicate CodexRegistrationWorkflowState = "duplicate"
	CodexRegistrationWorkflowImported  CodexRegistrationWorkflowState = "imported"
)

type CodexRegistrationCandidateRepository interface {
	List(ctx context.Context) ([]CodexRegistrationCandidateState, error)
	GetByID(ctx context.Context, id int64) (*CodexRegistrationCandidateState, error)
	ListByIDs(ctx context.Context, ids []int64) ([]CodexRegistrationCandidateState, error)
	UpsertBySourcePath(ctx context.Context, candidate CodexRegistrationCandidateState) (*CodexRegistrationCandidateState, error)
	Update(ctx context.Context, candidate CodexRegistrationCandidateState, expected *CodexRegistrationCandidateState) (*CodexRegistrationCandidateState, error)
	UpdateBatch(ctx context.Context, candidates []CodexRegistrationCandidateState, expected map[int64]CodexRegistrationCandidateState) error
	DeleteNonImportedBySourcePathsNotIn(ctx context.Context, sourcePaths []string, checkedAt time.Time) error
	DeleteAll(ctx context.Context) (int, error)
}

type CodexRegistrationAccountReader interface {
	ListByPlatform(ctx context.Context, platform string) ([]Account, error)
}

type CodexRegistrationOAuthRefresher interface {
	RefreshTokenWithClientID(ctx context.Context, refreshToken, proxyURL, clientID string) (*OpenAITokenInfo, error)
	BuildAccountCredentials(tokenInfo *OpenAITokenInfo) map[string]any
}

type CodexRegistrationAccountCreator interface {
	CreateAccount(ctx context.Context, input *CreateAccountInput) (*Account, error)
}

type CodexRegistrationImportInput struct {
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

type CodexRegistrationImportResult struct {
	ImportedIDs []int64          `json:"imported_ids"`
	Failed      map[int64]string `json:"failed"`
}

type CodexRegistrationService struct {
	repo           CodexRegistrationCandidateRepository
	accountReader  CodexRegistrationAccountReader
	oauth          CodexRegistrationOAuthRefresher
	accountCreator CodexRegistrationAccountCreator
	opMu           sync.Mutex
	tokenSnapshots map[string]codexRegistrationTokenSnapshot

	sourceDir    string
	probeURL     string
	scanWorkers  int
	probeTimeout time.Duration

	httpClient *http.Client
	now        func() time.Time
}

type codexRegistrationTokenSnapshot struct {
	SourceFingerprint string
	TokenInfo         *OpenAITokenInfo
}

func NewCodexRegistrationService(
	cfg *config.Config,
	repo CodexRegistrationCandidateRepository,
	accountReader CodexRegistrationAccountReader,
	oauth CodexRegistrationOAuthRefresher,
	accountCreator CodexRegistrationAccountCreator,
) *CodexRegistrationService {
	scanWorkers := defaultCodexRegistrationScanWorkers
	probeTimeout := defaultCodexRegistrationProbeTimeout

	if cfg != nil {
		if cfg.CodexRegistration.ScanWorkers > 0 {
			scanWorkers = cfg.CodexRegistration.ScanWorkers
		}
		if cfg.CodexRegistration.ProbeTimeoutSeconds > 0 {
			probeTimeout = time.Duration(cfg.CodexRegistration.ProbeTimeoutSeconds) * time.Second
		}
	}

	return &CodexRegistrationService{
		repo:           repo,
		accountReader:  accountReader,
		oauth:          oauth,
		accountCreator: accountCreator,
		tokenSnapshots: make(map[string]codexRegistrationTokenSnapshot),
		sourceDir:      defaultCodexRegistrationSourceDir,
		probeURL:       defaultCodexRegistrationProbeURL,
		scanWorkers:    scanWorkers,
		probeTimeout:   probeTimeout,
		httpClient:     &http.Client{},
		now:            time.Now,
	}
}

func CodexRegistrationLivenessStatuses() []CodexRegistrationLivenessStatus {
	return []CodexRegistrationLivenessStatus{
		CodexRegistrationLivenessAlive,
		CodexRegistrationLivenessDead,
		CodexRegistrationLivenessInvalid,
		CodexRegistrationLivenessError,
	}
}

func CodexRegistrationWorkflowStates() []CodexRegistrationWorkflowState {
	return []CodexRegistrationWorkflowState{
		CodexRegistrationWorkflowDetected,
		CodexRegistrationWorkflowStaged,
		CodexRegistrationWorkflowDuplicate,
		CodexRegistrationWorkflowImported,
	}
}

type CodexRegistrationScanCandidate struct {
	SourcePath        string
	SourceFilename    string
	SourceMtime       time.Time
	SourceFingerprint string
	Email             string
	AccountID         string
	Type              string
	ExpiresAt         *time.Time
	LastRefreshAt     *time.Time
	LivenessStatus    CodexRegistrationLivenessStatus
	StatusReason      string
	TokenInfo         *OpenAITokenInfo
}

type CodexRegistrationCandidateState struct {
	ID                int64
	SourcePath        string
	SourceFilename    string
	SourceMtime       time.Time
	SourceFingerprint string
	Email             string
	AccountID         string
	Type              string
	ExpiresAt         *time.Time
	LastRefreshAt     *time.Time
	LivenessStatus    CodexRegistrationLivenessStatus
	WorkflowState     CodexRegistrationWorkflowState
	StatusReason      string
	LastCheckedAt     time.Time
	ImportedAccountID *int64
	ImportedAt        *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type codexRegistrationSourceRecord struct {
	Type         string
	Email        string
	AccountID    string
	RefreshToken string
	AccessToken  string
	IDToken      string
	ClientID     string
	ExpiresAt    *time.Time
	LastRefresh  *time.Time
}

type codexRegistrationScanResult struct {
	candidate *CodexRegistrationScanCandidate
}

type codexRegistrationRefreshProbeResult struct {
	tokenInfo               *OpenAITokenInfo
	liveness                CodexRegistrationLivenessStatus
	statusReason            string
	expiresAt               *time.Time
	lastRefreshAt           *time.Time
	usedExistingAccessToken bool
}

type codexRegistrationDuplicateIndex struct {
	accountIDs map[string]struct{}
	emails     map[string]struct{}
}

func (s *CodexRegistrationService) Scan(ctx context.Context, model string) ([]CodexRegistrationCandidateState, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()

	if s.repo == nil {
		return nil, fmt.Errorf("codex registration repository is not configured")
	}
	existing, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}

	existingMap := make(map[string]CodexRegistrationCandidateState, len(existing))
	for i := range existing {
		existingMap[existing[i].SourcePath] = existing[i]
	}

	checkedAt := s.now().UTC()
	scanned, err := s.scanCandidates(ctx, model)
	if err != nil {
		return nil, err
	}

	next := ApplyCodexRegistrationScan(existingMap, scanned, checkedAt)

	if s.accountReader != nil {
		accounts, accountErr := s.accountReader.ListByPlatform(ctx, PlatformOpenAI)
		if accountErr != nil {
			return nil, accountErr
		}
		index := buildCodexRegistrationDuplicateIndex(accounts)
		applyCodexRegistrationExistingAccountDuplicates(next, index, checkedAt)
	}

	scannedPaths := make([]string, 0, len(scanned))
	for i := range scanned {
		scannedPaths = append(scannedPaths, scanned[i].SourcePath)
	}
	sort.Strings(scannedPaths)

	for _, path := range scannedPaths {
		state := next[path]
		saved, saveErr := s.repo.UpsertBySourcePath(ctx, state)
		if saveErr != nil {
			return nil, saveErr
		}
		next[path] = *saved
		if state.LivenessStatus == CodexRegistrationLivenessAlive {
			if scannedCandidate := findCodexRegistrationScanCandidateByPath(scanned, path); scannedCandidate != nil && scannedCandidate.TokenInfo != nil {
				s.tokenSnapshots[path] = codexRegistrationTokenSnapshot{
					SourceFingerprint: state.SourceFingerprint,
					TokenInfo:         scannedCandidate.TokenInfo,
				}
			}
		} else {
			delete(s.tokenSnapshots, path)
		}
	}
	if deleteErr := s.repo.DeleteNonImportedBySourcePathsNotIn(ctx, scannedPaths, checkedAt); deleteErr != nil {
		return nil, deleteErr
	}

	result := make([]CodexRegistrationCandidateState, 0, len(scannedPaths))
	for _, path := range scannedPaths {
		result = append(result, next[path])
	}
	return result, nil
}

func (s *CodexRegistrationService) Stage(ctx context.Context, candidateIDs []int64) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()

	if s.repo == nil {
		return fmt.Errorf("codex registration repository is not configured")
	}
	uniqueIDs := uniqueInt64(candidateIDs)
	if len(uniqueIDs) == 0 {
		return infraerrors.BadRequest(codexRegistrationReasonInvalidCandidateSelection, "candidate_ids is required")
	}
	candidates, err := s.repo.ListByIDs(ctx, uniqueIDs)
	if err != nil {
		return err
	}
	if len(candidates) != len(uniqueIDs) {
		return infraerrors.BadRequest(codexRegistrationReasonCandidateNotFound, "some selected candidates do not exist")
	}
	byID := make(map[int64]CodexRegistrationCandidateState, len(candidates))
	for i := range candidates {
		byID[candidates[i].ID] = candidates[i]
	}
	now := s.now().UTC()
	stagedCandidates := make([]CodexRegistrationCandidateState, 0, len(uniqueIDs))
	for _, candidateID := range uniqueIDs {
		candidate := byID[candidateID]
		staged, stageErr := StageCodexRegistrationCandidate(candidate, now)
		if stageErr != nil {
			return stageErr
		}
		stagedCandidates = append(stagedCandidates, staged)
	}
	return s.repo.UpdateBatch(ctx, stagedCandidates, byID)
}

func (s *CodexRegistrationService) Unstage(ctx context.Context, candidateIDs []int64) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()

	if s.repo == nil {
		return fmt.Errorf("codex registration repository is not configured")
	}
	uniqueIDs := uniqueInt64(candidateIDs)
	if len(uniqueIDs) == 0 {
		return infraerrors.BadRequest(codexRegistrationReasonInvalidCandidateSelection, "candidate_ids is required")
	}
	candidates, err := s.repo.ListByIDs(ctx, uniqueIDs)
	if err != nil {
		return err
	}
	if len(candidates) != len(uniqueIDs) {
		return infraerrors.BadRequest(codexRegistrationReasonCandidateNotFound, "some selected candidates do not exist")
	}
	byID := make(map[int64]CodexRegistrationCandidateState, len(candidates))
	for i := range candidates {
		byID[candidates[i].ID] = candidates[i]
	}
	now := s.now().UTC()
	unstagedCandidates := make([]CodexRegistrationCandidateState, 0, len(uniqueIDs))
	for _, candidateID := range uniqueIDs {
		candidate := byID[candidateID]
		unstaged, unstageErr := UnstageCodexRegistrationCandidate(candidate, now)
		if unstageErr != nil {
			return unstageErr
		}
		unstagedCandidates = append(unstagedCandidates, unstaged)
	}
	return s.repo.UpdateBatch(ctx, unstagedCandidates, byID)
}

func (s *CodexRegistrationService) Import(ctx context.Context, input CodexRegistrationImportInput) (*CodexRegistrationImportResult, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()

	if s.repo == nil {
		return nil, fmt.Errorf("codex registration repository is not configured")
	}
	if s.oauth == nil {
		return nil, fmt.Errorf("codex registration oauth refresher is not configured")
	}
	if s.accountCreator == nil {
		return nil, fmt.Errorf("codex registration account creator is not configured")
	}
	if len(input.GroupIDs) == 0 {
		return nil, infraerrors.BadRequest(codexRegistrationReasonInvalidGroupSelection, "at least one group_id is required")
	}
	selectedIDs := uniqueInt64(input.CandidateIDs)
	if len(selectedIDs) == 0 {
		return nil, infraerrors.BadRequest(codexRegistrationReasonInvalidCandidateSelection, "candidate_ids is required")
	}

	candidates, err := s.repo.ListByIDs(ctx, selectedIDs)
	if err != nil {
		return nil, err
	}
	if len(candidates) != len(selectedIDs) {
		return nil, infraerrors.BadRequest(codexRegistrationReasonCandidateNotFound, "some selected candidates do not exist")
	}

	byID := make(map[int64]CodexRegistrationCandidateState, len(candidates))
	for i := range candidates {
		byID[candidates[i].ID] = candidates[i]
	}

	duplicateIndex, duplicateErr := s.loadCodexRegistrationDuplicateIndex(ctx)
	if duplicateErr != nil {
		return nil, duplicateErr
	}

	result := &CodexRegistrationImportResult{
		ImportedIDs: make([]int64, 0, len(selectedIDs)),
		Failed:      make(map[int64]string),
	}

	importConcurrency, importLoadFactor, importPriority, importRateMultiplier := normalizeCodexRegistrationImportAccountSettings(input)
	selectedGroupIDs := append([]int64(nil), input.GroupIDs...)
	now := s.now().UTC()
	for _, id := range selectedIDs {
		candidate := byID[id]
		snapshot := candidate

		if candidate.LivenessStatus != CodexRegistrationLivenessAlive {
			if strings.TrimSpace(candidate.StatusReason) != "" {
				result.Failed[id] = candidate.StatusReason
			} else {
				result.Failed[id] = "candidate is not alive"
			}
			continue
		}

		sourceRecord, sourceMeta, sourceErr := s.loadSourceRecord(candidate.SourcePath)
		if sourceErr != nil {
			downgraded := candidate
			downgraded.LivenessStatus = CodexRegistrationLivenessInvalid
			downgraded.WorkflowState = CodexRegistrationWorkflowDetected
			downgraded.StatusReason = fmt.Sprintf("invalid source: %v", sourceErr)
			downgraded.LastCheckedAt = now
			downgraded.UpdatedAt = now
			if _, updateErr := s.repo.Update(ctx, downgraded, &snapshot); updateErr != nil {
				return nil, updateErr
			}
			result.Failed[id] = downgraded.StatusReason
			continue
		}
		if candidate.SourceFingerprint != "" && candidate.SourceFingerprint != sourceMeta.SourceFingerprint {
			downgraded := candidate
			downgraded.WorkflowState = CodexRegistrationWorkflowDetected
			downgraded.StatusReason = "source changed since staging; rescan required"
			downgraded.LastCheckedAt = now
			downgraded.UpdatedAt = now
			if _, updateErr := s.repo.Update(ctx, downgraded, &snapshot); updateErr != nil {
				return nil, updateErr
			}
			result.Failed[id] = downgraded.StatusReason
			continue
		}

		candidate.SourceFilename = sourceMeta.SourceFilename
		candidate.SourcePath = sourceMeta.SourcePath
		candidate.SourceMtime = sourceMeta.SourceMtime
		candidate.SourceFingerprint = sourceMeta.SourceFingerprint
		candidate.Email = sourceRecord.Email
		candidate.AccountID = sourceRecord.AccountID
		candidate.Type = sourceRecord.Type

		if sourceRecord.Type != "codex" {
			candidate.LivenessStatus = CodexRegistrationLivenessInvalid
			candidate.WorkflowState = CodexRegistrationWorkflowDetected
			candidate.StatusReason = fmt.Sprintf("invalid source type: %s", sourceRecord.Type)
			candidate.LastCheckedAt = now
			candidate.UpdatedAt = now
			if _, updateErr := s.repo.Update(ctx, candidate, &snapshot); updateErr != nil {
				return nil, updateErr
			}
			result.Failed[id] = candidate.StatusReason
			continue
		}

		effectiveSource := sourceRecord
		if snapshot, ok := s.tokenSnapshots[candidate.SourcePath]; ok && snapshot.TokenInfo != nil && snapshot.SourceFingerprint == sourceMeta.SourceFingerprint {
			if strings.TrimSpace(snapshot.TokenInfo.RefreshToken) != "" {
				effectiveSource.RefreshToken = strings.TrimSpace(snapshot.TokenInfo.RefreshToken)
			}
			if strings.TrimSpace(snapshot.TokenInfo.IDToken) != "" {
				effectiveSource.IDToken = strings.TrimSpace(snapshot.TokenInfo.IDToken)
			}
			if strings.TrimSpace(snapshot.TokenInfo.ClientID) != "" {
				effectiveSource.ClientID = strings.TrimSpace(snapshot.TokenInfo.ClientID)
			}
			if strings.TrimSpace(snapshot.TokenInfo.Email) != "" {
				effectiveSource.Email = strings.TrimSpace(snapshot.TokenInfo.Email)
			}
			if strings.TrimSpace(snapshot.TokenInfo.ChatGPTAccountID) != "" {
				effectiveSource.AccountID = strings.TrimSpace(snapshot.TokenInfo.ChatGPTAccountID)
			}
		}

			revalidate, probeErr := s.refreshAndProbe(ctx, effectiveSource, "gpt-5.4-mini")
		if probeErr != nil {
			downgraded := candidate
			downgraded.LivenessStatus = CodexRegistrationLivenessError
			downgraded.WorkflowState = CodexRegistrationWorkflowDetected
			downgraded.StatusReason = probeErr.Error()
			downgraded.LastCheckedAt = now
			downgraded.UpdatedAt = now
			if _, updateErr := s.repo.Update(ctx, downgraded, &snapshot); updateErr != nil {
				return nil, updateErr
			}
			result.Failed[id] = downgraded.StatusReason
			continue
		}

		candidate.ExpiresAt = revalidate.expiresAt
		candidate.LastRefreshAt = revalidate.lastRefreshAt
		applyCodexRegistrationCanonicalIdentityToState(&candidate, effectiveSource, revalidate.tokenInfo)
		candidate.LivenessStatus = revalidate.liveness
		candidate.StatusReason = revalidate.statusReason
		candidate.LastCheckedAt = now
		if revalidate.tokenInfo != nil {
			s.tokenSnapshots[candidate.SourcePath] = codexRegistrationTokenSnapshot{
				SourceFingerprint: candidate.SourceFingerprint,
				TokenInfo:         revalidate.tokenInfo,
			}
		}

		if revalidate.liveness != CodexRegistrationLivenessAlive {
			candidate.WorkflowState = CodexRegistrationWorkflowDetected
			candidate.UpdatedAt = now
			if _, updateErr := s.repo.Update(ctx, candidate, &snapshot); updateErr != nil {
				return nil, updateErr
			}
			result.Failed[id] = revalidate.statusReason
			continue
		}

		if isCodexRegistrationDuplicate(candidate, duplicateIndex) {
			candidate.WorkflowState = CodexRegistrationWorkflowDuplicate
			candidate.UpdatedAt = now
			if _, updateErr := s.repo.Update(ctx, candidate, &snapshot); updateErr != nil {
				return nil, updateErr
			}
			result.Failed[id] = "duplicate account already exists"
			continue
		}

		// Re-check duplicates immediately before account creation to avoid stale reads.
		latestDuplicateIndex, latestDuplicateErr := s.loadCodexRegistrationDuplicateIndex(ctx)
		if latestDuplicateErr != nil {
			return nil, latestDuplicateErr
		}
		mergeCodexRegistrationDuplicateIndex(latestDuplicateIndex, duplicateIndex)
		duplicateIndex = latestDuplicateIndex
		if isCodexRegistrationDuplicate(candidate, duplicateIndex) {
			candidate.WorkflowState = CodexRegistrationWorkflowDuplicate
			candidate.UpdatedAt = now
			if _, updateErr := s.repo.Update(ctx, candidate, &snapshot); updateErr != nil {
				return nil, updateErr
			}
			result.Failed[id] = "duplicate account already exists"
			continue
		}

		credentials := s.oauth.BuildAccountCredentials(revalidate.tokenInfo)
		if credentials == nil {
			credentials = map[string]any{}
		}
		if strings.TrimSpace(sourceRecord.RefreshToken) != "" && !revalidate.usedExistingAccessToken {
			if _, ok := credentials["refresh_token"]; !ok {
				credentials["refresh_token"] = strings.TrimSpace(sourceRecord.RefreshToken)
			}
		}
		if strings.TrimSpace(sourceRecord.IDToken) != "" {
			if _, ok := credentials["id_token"]; !ok {
				credentials["id_token"] = strings.TrimSpace(sourceRecord.IDToken)
			}
		}
		if strings.TrimSpace(sourceRecord.Email) != "" {
			if _, ok := credentials["email"]; !ok {
				credentials["email"] = strings.TrimSpace(sourceRecord.Email)
			}
		}
		effectiveAccountID := strings.TrimSpace(candidate.AccountID)
		if effectiveAccountID == "" {
			effectiveAccountID = strings.TrimSpace(sourceRecord.AccountID)
		}
		if effectiveAccountID != "" {
			if _, ok := credentials["chatgpt_account_id"]; !ok {
				credentials["chatgpt_account_id"] = effectiveAccountID
			}
			if _, ok := credentials["account_id"]; !ok {
				credentials["account_id"] = effectiveAccountID
			}
		}
		if strings.TrimSpace(sourceRecord.ClientID) != "" {
			if _, ok := credentials["client_id"]; !ok {
				credentials["client_id"] = strings.TrimSpace(sourceRecord.ClientID)
			}
		}
		if input.ImportModels {
			modelMapping, importModelErr := s.importOpenAIModelMapping(ctx, credentials)
			if importModelErr == nil && len(modelMapping) > 0 {
				credentials["model_mapping"] = modelMapping
			}
		}

		notes := strings.TrimSpace(input.Notes)
		var notesPtr *string
		if notes != "" {
			notesPtr = &notes
		}

		accountNameEmail := strings.TrimSpace(candidate.Email)
		if accountNameEmail == "" {
			accountNameEmail = strings.TrimSpace(sourceRecord.Email)
		}
		accountName := "codex:" + accountNameEmail

		account, createErr := s.accountCreator.CreateAccount(ctx, &CreateAccountInput{
			Name:                 accountName,
			Notes:                notesPtr,
			Platform:             PlatformOpenAI,
			Type:                 AccountTypeOAuth,
			Credentials:          credentials,
			ProxyID:              input.ProxyID,
			Concurrency:          importConcurrency,
			LoadFactor:           importLoadFactor,
			Priority:             importPriority,
			RateMultiplier:       importRateMultiplier,
			GroupIDs:             selectedGroupIDs,
			SkipDefaultGroupBind: true,
		})
		if createErr != nil {
			candidate.WorkflowState = CodexRegistrationWorkflowDetected
			candidate.UpdatedAt = now
			candidate.StatusReason = fmt.Sprintf("create account failed: %v", createErr)
			if _, updateErr := s.repo.Update(ctx, candidate, &snapshot); updateErr != nil {
				return nil, updateErr
			}
			result.Failed[id] = candidate.StatusReason
			continue
		}

		imported, markErr := MarkCodexRegistrationCandidateImported(candidate, account.ID, now)
		if markErr != nil {
			return nil, markErr
		}
		imported.LivenessStatus = CodexRegistrationLivenessAlive
		imported.StatusReason = "imported"
		imported.LastCheckedAt = now
		if _, updateErr := s.repo.Update(ctx, imported, &snapshot); updateErr != nil {
			current, currentErr := s.repo.GetByID(ctx, id)
			if currentErr != nil {
				return nil, currentErr
			}
			if current == nil {
				result.ImportedIDs = append(result.ImportedIDs, id)
				duplicateIndex.accountIDs[normalizeCodexRegistrationIdentity(candidate.AccountID)] = struct{}{}
				duplicateIndex.emails[normalizeCodexRegistrationIdentity(candidate.Email)] = struct{}{}
				continue
			}
			fallbackImported, fallbackErr := MarkCodexRegistrationCandidateImported(*current, account.ID, now)
			if fallbackErr != nil {
				return nil, fallbackErr
			}
			fallbackImported.LivenessStatus = CodexRegistrationLivenessAlive
			fallbackImported.StatusReason = "imported"
			fallbackImported.LastCheckedAt = now
			if _, fallbackUpdateErr := s.repo.Update(ctx, fallbackImported, nil); fallbackUpdateErr != nil {
				return nil, fallbackUpdateErr
			}
		}

		result.ImportedIDs = append(result.ImportedIDs, id)
		duplicateIndex.accountIDs[normalizeCodexRegistrationIdentity(candidate.AccountID)] = struct{}{}
		duplicateIndex.emails[normalizeCodexRegistrationIdentity(candidate.Email)] = struct{}{}
	}

	return result, nil
}

func normalizeCodexRegistrationImportAccountSettings(input CodexRegistrationImportInput) (int, *int, int, *float64) {
	concurrency := 1
	if input.Concurrency != nil && *input.Concurrency > 0 {
		concurrency = *input.Concurrency
	}

	loadFactorValue := 1
	if input.LoadFactor != nil && *input.LoadFactor > 0 {
		loadFactorValue = *input.LoadFactor
	}
	loadFactor := &loadFactorValue

	priority := 1
	if input.Priority != nil && *input.Priority > 0 {
		priority = *input.Priority
	}

	rateMultiplierValue := 1.0
	if input.RateMultiplier != nil {
		rateMultiplierValue = *input.RateMultiplier
	}
	rateMultiplier := &rateMultiplierValue

	return concurrency, loadFactor, priority, rateMultiplier
}

func (s *CodexRegistrationService) importOpenAIModelMapping(_ context.Context, credentials map[string]any) (map[string]string, error) {
	accessToken := strings.TrimSpace(readStringFromMap(credentials, "access_token"))
	if accessToken == "" {
		return nil, fmt.Errorf("missing access_token for model import")
	}

	return buildIdentityModelMapping(openai.DefaultModelIDs()), nil
}

func buildIdentityModelMapping(models []string) map[string]string {
	if len(models) == 0 {
		return nil
	}

	modelMapping := make(map[string]string, len(models))
	for _, model := range models {
		modelID := strings.TrimSpace(model)
		if modelID == "" {
			continue
		}
		modelMapping[modelID] = modelID
	}
	if len(modelMapping) == 0 {
		return nil
	}
	return modelMapping
}

func (s *CodexRegistrationService) loadCodexRegistrationDuplicateIndex(ctx context.Context) (codexRegistrationDuplicateIndex, error) {
	if s.accountReader == nil {
		return codexRegistrationDuplicateIndex{
			accountIDs: map[string]struct{}{},
			emails:     map[string]struct{}{},
		}, nil
	}
	accounts, err := s.accountReader.ListByPlatform(ctx, PlatformOpenAI)
	if err != nil {
		return codexRegistrationDuplicateIndex{}, err
	}
	return buildCodexRegistrationDuplicateIndex(accounts), nil
}

func mergeCodexRegistrationDuplicateIndex(target, from codexRegistrationDuplicateIndex) {
	for accountID := range from.accountIDs {
		target.accountIDs[accountID] = struct{}{}
	}
	for email := range from.emails {
		target.emails[email] = struct{}{}
	}
}

func (s *CodexRegistrationService) scanCandidates(ctx context.Context, model string) ([]CodexRegistrationScanCandidate, error) {
	entries, err := os.ReadDir(s.sourceDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []CodexRegistrationScanCandidate{}, nil
		}
		return nil, err
	}

	paths := make([]string, 0, len(entries))
	for i := range entries {
		if entries[i].IsDir() {
			continue
		}
		name := entries[i].Name()
		if !strings.HasSuffix(strings.ToLower(name), ".json") {
			continue
		}
		paths = append(paths, filepath.Join(s.sourceDir, name))
	}

	if len(paths) == 0 {
		return []CodexRegistrationScanCandidate{}, nil
	}

	workerCount := s.scanWorkers
	if workerCount <= 0 {
		workerCount = defaultCodexRegistrationScanWorkers
	}
	if workerCount > len(paths) {
		workerCount = len(paths)
	}

	jobs := make(chan string)
	results := make(chan codexRegistrationScanResult, len(paths))
	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				candidate, include := s.scanSingleFile(ctx, path, model)
				if include {
					results <- codexRegistrationScanResult{candidate: candidate}
				}
			}
		}()
	}

	for _, path := range paths {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			close(results)
			return nil, ctx.Err()
		case jobs <- path:
		}
	}
	close(jobs)
	wg.Wait()
	close(results)

	scanned := make([]CodexRegistrationScanCandidate, 0, len(results))
	for result := range results {
		if result.candidate != nil {
			scanned = append(scanned, *result.candidate)
		}
	}
	sort.Slice(scanned, func(i, j int) bool {
		return scanned[i].SourcePath < scanned[j].SourcePath
	})
	return scanned, nil
}

func (s *CodexRegistrationService) scanSingleFile(ctx context.Context, path string, model string) (*CodexRegistrationScanCandidate, bool) {
	source, metadata, err := s.loadSourceRecord(path)
	if err != nil {
		invalid := metadata
		invalid.LivenessStatus = CodexRegistrationLivenessInvalid
		invalid.StatusReason = err.Error()
		return &invalid, true
	}
	if source.Type != "codex" {
		return nil, false
	}

	candidate := metadata
	candidate.Type = source.Type
	candidate.Email = source.Email
	candidate.AccountID = source.AccountID
	candidate.ExpiresAt = source.ExpiresAt
	candidate.LastRefreshAt = source.LastRefresh

	revalidate, refreshErr := s.refreshAndProbe(ctx, source, model)
	if refreshErr != nil {
		candidate.LivenessStatus = CodexRegistrationLivenessError
		candidate.StatusReason = refreshErr.Error()
		return &candidate, true
	}

	applyCodexRegistrationCanonicalIdentity(&candidate, source, revalidate.tokenInfo)
	candidate.LivenessStatus = revalidate.liveness
	candidate.StatusReason = revalidate.statusReason
	candidate.TokenInfo = revalidate.tokenInfo
	if revalidate.expiresAt != nil {
		candidate.ExpiresAt = revalidate.expiresAt
	}
	if revalidate.lastRefreshAt != nil {
		candidate.LastRefreshAt = revalidate.lastRefreshAt
	}
	return &candidate, true
}

func findCodexRegistrationScanCandidateByPath(candidates []CodexRegistrationScanCandidate, path string) *CodexRegistrationScanCandidate {
	for i := range candidates {
		if candidates[i].SourcePath == path {
			return &candidates[i]
		}
	}
	return nil
}

func applyCodexRegistrationCanonicalIdentity(candidate *CodexRegistrationScanCandidate, source codexRegistrationSourceRecord, tokenInfo *OpenAITokenInfo) {
	email := strings.TrimSpace(source.Email)
	accountID := strings.TrimSpace(source.AccountID)

	if tokenInfo != nil {
		if refreshedEmail := strings.TrimSpace(tokenInfo.Email); refreshedEmail != "" {
			email = refreshedEmail
		}
		if refreshedAccountID := strings.TrimSpace(tokenInfo.ChatGPTAccountID); refreshedAccountID != "" {
			accountID = refreshedAccountID
		}
	}

	candidate.Email = email
	candidate.AccountID = accountID
}

func applyCodexRegistrationCanonicalIdentityToState(candidate *CodexRegistrationCandidateState, source codexRegistrationSourceRecord, tokenInfo *OpenAITokenInfo) {
	email := strings.TrimSpace(source.Email)
	accountID := strings.TrimSpace(source.AccountID)

	if tokenInfo != nil {
		if refreshedEmail := strings.TrimSpace(tokenInfo.Email); refreshedEmail != "" {
			email = refreshedEmail
		}
		if refreshedAccountID := strings.TrimSpace(tokenInfo.ChatGPTAccountID); refreshedAccountID != "" {
			accountID = refreshedAccountID
		}
	}

	candidate.Email = email
	candidate.AccountID = accountID
}

func (s *CodexRegistrationService) refreshAndProbe(ctx context.Context, source codexRegistrationSourceRecord, model string) (*codexRegistrationRefreshProbeResult, error) {
	if strings.TrimSpace(source.Email) == "" {
		return &codexRegistrationRefreshProbeResult{
			liveness:     CodexRegistrationLivenessInvalid,
			statusReason: "missing mandatory source fields: email",
		}, nil
	}
	if strings.TrimSpace(source.RefreshToken) == "" {
		if fallback := s.probeExistingAccessToken(ctx, source, nil, model); fallback != nil {
			return fallback, nil
		}
		return &codexRegistrationRefreshProbeResult{
			liveness:     CodexRegistrationLivenessInvalid,
			statusReason: "missing mandatory source fields: refresh_token/access_token",
		}, nil
	}
	if s.oauth == nil {
		return nil, fmt.Errorf("codex registration oauth refresher is not configured")
	}

	refreshCtx, cancelRefresh := context.WithTimeout(ctx, s.probeTimeout)
	defer cancelRefresh()
	tokenInfo, err := s.oauth.RefreshTokenWithClientID(
		refreshCtx,
		strings.TrimSpace(source.RefreshToken),
		"",
		strings.TrimSpace(source.ClientID),
	)
	if err != nil {
		if isCodexRegistrationAuthFailure(err) {
			if fallback := s.probeExistingAccessToken(ctx, source, err, model); fallback != nil {
				return fallback, nil
			}
			return &codexRegistrationRefreshProbeResult{
				liveness:     CodexRegistrationLivenessDead,
				statusReason: fmt.Sprintf("refresh auth failure: %v", err),
			}, nil
		}
		if isCodexRegistrationTimeout(err) {
			return &codexRegistrationRefreshProbeResult{
				liveness:     CodexRegistrationLivenessError,
				statusReason: fmt.Sprintf("refresh timeout: %v", err),
			}, nil
		}
		return &codexRegistrationRefreshProbeResult{
			liveness:     CodexRegistrationLivenessError,
			statusReason: fmt.Sprintf("refresh error: %v", err),
		}, nil
	}

	accessToken := ""
	if tokenInfo != nil {
		accessToken = strings.TrimSpace(tokenInfo.AccessToken)
	}
	if accessToken == "" {
		return &codexRegistrationRefreshProbeResult{
			liveness:     CodexRegistrationLivenessError,
			statusReason: "refresh succeeded but access_token is empty",
			tokenInfo:    tokenInfo,
		}, nil
	}

	probeCtx, cancelProbe := context.WithTimeout(ctx, s.probeTimeout)
	defer cancelProbe()
	liveness, reason := s.probeCodexAccessToken(probeCtx, accessToken, model)
	now := s.now().UTC()
	result := &codexRegistrationRefreshProbeResult{
		tokenInfo:     tokenInfo,
		liveness:      liveness,
		statusReason:  reason,
		lastRefreshAt: &now,
	}
	if tokenInfo != nil && tokenInfo.ExpiresAt > 0 {
		expires := time.Unix(tokenInfo.ExpiresAt, 0).UTC()
		result.expiresAt = &expires
	}
	return result, nil
}

func buildCodexRegistrationSourceTokenInfo(source codexRegistrationSourceRecord, includeRefreshToken bool) *OpenAITokenInfo {
	accessToken := strings.TrimSpace(source.AccessToken)
	if accessToken == "" {
		return nil
	}

	tokenInfo := &OpenAITokenInfo{
		AccessToken:      accessToken,
		IDToken:          strings.TrimSpace(source.IDToken),
		ClientID:         strings.TrimSpace(source.ClientID),
		Email:            strings.TrimSpace(source.Email),
		ChatGPTAccountID: strings.TrimSpace(source.AccountID),
	}
	if includeRefreshToken && strings.TrimSpace(source.RefreshToken) != "" {
		tokenInfo.RefreshToken = strings.TrimSpace(source.RefreshToken)
	}
	if source.ExpiresAt != nil {
		tokenInfo.ExpiresAt = source.ExpiresAt.Unix()
		tokenInfo.ExpiresIn = int64(time.Until(*source.ExpiresAt).Seconds())
	}
	return tokenInfo
}

func (s *CodexRegistrationService) probeExistingAccessToken(ctx context.Context, source codexRegistrationSourceRecord, refreshErr error, model string) *codexRegistrationRefreshProbeResult {
	accessToken := strings.TrimSpace(source.AccessToken)
	if accessToken == "" {
		return nil
	}

	probeCtx, cancelProbe := context.WithTimeout(ctx, s.probeTimeout)
	defer cancelProbe()
	liveness, reason := s.probeCodexAccessToken(probeCtx, accessToken, model)
	statusReason := fmt.Sprintf("%s via existing access_token", reason)
	if refreshErr != nil {
		statusReason = fmt.Sprintf("%s via existing access_token after refresh auth failure: %v", reason, refreshErr)
	}

	return &codexRegistrationRefreshProbeResult{
		tokenInfo:               buildCodexRegistrationSourceTokenInfo(source, false),
		liveness:                liveness,
		statusReason:            statusReason,
		expiresAt:               source.ExpiresAt,
		lastRefreshAt:           source.LastRefresh,
		usedExistingAccessToken: true,
	}
}

func (s *CodexRegistrationService) probeCodexAccessToken(ctx context.Context, accessToken string, model string) (CodexRegistrationLivenessStatus, string) {
	probeModel := strings.TrimSpace(model)
	if probeModel == "" {
		probeModel = defaultCodexRegistrationProbeModel
	}
	payload := map[string]any{
		"model":        probeModel,
		"instructions": "ping",
		"input": []map[string]string{
			{
				"role":    "user",
				"content": "ping",
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return CodexRegistrationLivenessError, fmt.Sprintf("probe payload marshal failed: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.probeURL, bytes.NewReader(body))
	if err != nil {
		return CodexRegistrationLivenessError, fmt.Sprintf("probe request build failed: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", codexRegistrationProbeRequestMimeType)
	req.Header.Set("User-Agent", codexRegistrationProbeUserAgent)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		if isCodexRegistrationTimeout(err) {
			return CodexRegistrationLivenessError, fmt.Sprintf("probe timeout: %v", err)
		}
		return CodexRegistrationLivenessError, fmt.Sprintf("probe request failed: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return CodexRegistrationLivenessError, fmt.Sprintf("probe read failed: %v", err)
	}
	lowerBody := strings.ToLower(string(respBody))
	if resp.StatusCode == http.StatusUnauthorized || strings.Contains(lowerBody, "status\":401") {
		return CodexRegistrationLivenessDead, "probe unauthorized"
	}
	if hasCodexRegistrationAuthMarker(lowerBody) {
		return CodexRegistrationLivenessDead, "probe auth marker detected"
	}
	if resp.StatusCode == http.StatusTooManyRequests && strings.Contains(lowerBody, "usage_limit_reached") {
		return CodexRegistrationLivenessAlive, "probe usage limit reached"
	}
	if resp.StatusCode >= http.StatusInternalServerError {
		return CodexRegistrationLivenessError, fmt.Sprintf("probe upstream status=%d", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return CodexRegistrationLivenessError, fmt.Sprintf("probe unexpected status=%d", resp.StatusCode)
	}
	if !json.Valid(respBody) {
		return CodexRegistrationLivenessError, "probe malformed response"
	}
	return CodexRegistrationLivenessAlive, "probe ok"
}

func (s *CodexRegistrationService) loadSourceRecord(path string) (codexRegistrationSourceRecord, CodexRegistrationScanCandidate, error) {
	record := codexRegistrationSourceRecord{}
	metadata := CodexRegistrationScanCandidate{
		SourcePath:     path,
		SourceFilename: filepath.Base(path),
	}

	info, err := os.Stat(path)
	if err != nil {
		return record, metadata, fmt.Errorf("stat source file: %w", err)
	}
	metadata.SourceMtime = info.ModTime().UTC()

	raw, err := os.ReadFile(path)
	if err != nil {
		return record, metadata, fmt.Errorf("read source file: %w", err)
	}
	sum := sha256.Sum256(raw)
	metadata.SourceFingerprint = hex.EncodeToString(sum[:])

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return record, metadata, fmt.Errorf("invalid json: %w", err)
	}

	record.Type = strings.ToLower(strings.TrimSpace(readStringFromMap(payload, "type")))
	record.Email = strings.TrimSpace(readStringFromMap(payload, "email"))
	record.AccountID = strings.TrimSpace(readStringFromMap(payload, "account_id"))
	record.RefreshToken = strings.TrimSpace(readStringFromMap(payload, "refresh_token"))
	record.AccessToken = strings.TrimSpace(readStringFromMap(payload, "access_token"))
	record.IDToken = strings.TrimSpace(readStringFromMap(payload, "id_token"))
	record.ClientID = strings.TrimSpace(readStringFromMap(payload, "client_id"))
	record.ExpiresAt = readTimeFromMap(payload, "expired", "expires_at")
	record.LastRefresh = readTimeFromMap(payload, "last_refresh", "last_refresh_at")

	if record.Type == "codex" {
		if record.Email == "" || (record.RefreshToken == "" && record.AccessToken == "") {
			return record, metadata, fmt.Errorf("missing mandatory source fields: refresh_token/access_token/email")
		}
	}
	return record, metadata, nil
}

func ApplyCodexRegistrationScan(
	existing map[string]CodexRegistrationCandidateState,
	scanned []CodexRegistrationScanCandidate,
	checkedAt time.Time,
) map[string]CodexRegistrationCandidateState {
	next := make(map[string]CodexRegistrationCandidateState, len(existing))
	for path, candidate := range existing {
		next[path] = candidate
	}

	accountPaths := make(map[string][]string)
	for _, candidate := range scanned {
		if candidate.SourcePath == "" {
			continue
		}

		previous, hasPrevious := next[candidate.SourcePath]
		workflow := CodexRegistrationWorkflowDetected
		createdAt := checkedAt
		importedAccountID := (*int64)(nil)
		importedAt := (*time.Time)(nil)
		id := int64(0)

		if hasPrevious {
			workflow = nextWorkflowState(previous, candidate)
			createdAt = previous.CreatedAt
			if createdAt.IsZero() {
				createdAt = checkedAt
			}
			importedAccountID = previous.ImportedAccountID
			importedAt = previous.ImportedAt
			id = previous.ID
		}

		next[candidate.SourcePath] = CodexRegistrationCandidateState{
			ID:                id,
			SourcePath:        candidate.SourcePath,
			SourceFilename:    candidate.SourceFilename,
			SourceMtime:       candidate.SourceMtime,
			SourceFingerprint: candidate.SourceFingerprint,
			Email:             candidate.Email,
			AccountID:         candidate.AccountID,
			Type:              candidate.Type,
			ExpiresAt:         candidate.ExpiresAt,
			LastRefreshAt:     candidate.LastRefreshAt,
			LivenessStatus:    candidate.LivenessStatus,
			WorkflowState:     workflow,
			StatusReason:      candidate.StatusReason,
			LastCheckedAt:     checkedAt,
			ImportedAccountID: importedAccountID,
			ImportedAt:        importedAt,
			CreatedAt:         createdAt,
			UpdatedAt:         checkedAt,
		}

		if candidate.LivenessStatus == CodexRegistrationLivenessAlive && candidate.AccountID != "" {
			accountPaths[candidate.AccountID] = append(accountPaths[candidate.AccountID], candidate.SourcePath)
		}
	}

	for _, paths := range accountPaths {
		if len(paths) < 2 {
			continue
		}
		for _, sourcePath := range paths {
			entry := next[sourcePath]
			if entry.WorkflowState == CodexRegistrationWorkflowImported {
				continue
			}
			entry.WorkflowState = CodexRegistrationWorkflowDuplicate
			entry.UpdatedAt = checkedAt
			next[sourcePath] = entry
		}
	}

	return next
}

func nextWorkflowState(
	previous CodexRegistrationCandidateState,
	scanned CodexRegistrationScanCandidate,
) CodexRegistrationWorkflowState {
	switch previous.WorkflowState {
	case CodexRegistrationWorkflowImported:
		return CodexRegistrationWorkflowImported
	case CodexRegistrationWorkflowStaged:
		if previous.SourceFingerprint == scanned.SourceFingerprint &&
			scanned.LivenessStatus == CodexRegistrationLivenessAlive {
			return CodexRegistrationWorkflowStaged
		}
	}

	return CodexRegistrationWorkflowDetected
}

func (c CodexRegistrationCandidateState) CanStage() bool {
	return c.LivenessStatus == CodexRegistrationLivenessAlive && c.WorkflowState == CodexRegistrationWorkflowDetected
}

func (c CodexRegistrationCandidateState) CanUnstage() bool {
	return c.WorkflowState == CodexRegistrationWorkflowStaged
}

func StageCodexRegistrationCandidate(candidate CodexRegistrationCandidateState, updatedAt time.Time) (CodexRegistrationCandidateState, error) {
	if !candidate.CanStage() {
		return candidate, infraerrors.BadRequest(codexRegistrationReasonInvalidStageTransition, fmt.Sprintf("candidate cannot be staged from state=%s liveness=%s", candidate.WorkflowState, candidate.LivenessStatus))
	}
	candidate.WorkflowState = CodexRegistrationWorkflowStaged
	candidate.UpdatedAt = updatedAt
	return candidate, nil
}

func UnstageCodexRegistrationCandidate(candidate CodexRegistrationCandidateState, updatedAt time.Time) (CodexRegistrationCandidateState, error) {
	if !candidate.CanUnstage() {
		return candidate, infraerrors.BadRequest(codexRegistrationReasonInvalidUnstageTransition, fmt.Sprintf("candidate cannot be unstaged from state=%s", candidate.WorkflowState))
	}
	candidate.WorkflowState = CodexRegistrationWorkflowDetected
	candidate.UpdatedAt = updatedAt
	return candidate, nil
}

func MarkCodexRegistrationCandidateImported(candidate CodexRegistrationCandidateState, importedAccountID int64, importedAt time.Time) (CodexRegistrationCandidateState, error) {
	if importedAccountID <= 0 {
		return candidate, fmt.Errorf("imported account id must be positive")
	}
	if importedAt.IsZero() {
		return candidate, fmt.Errorf("imported time must be set")
	}
	candidate.WorkflowState = CodexRegistrationWorkflowImported
	candidate.ImportedAccountID = &importedAccountID
	candidate.ImportedAt = &importedAt
	candidate.UpdatedAt = importedAt
	return candidate, nil
}

func buildCodexRegistrationDuplicateIndex(accounts []Account) codexRegistrationDuplicateIndex {
	index := codexRegistrationDuplicateIndex{
		accountIDs: map[string]struct{}{},
		emails:     map[string]struct{}{},
	}
	for i := range accounts {
		accountID := normalizeCodexRegistrationIdentity(accounts[i].GetCredential("chatgpt_account_id"))
		if accountID == "" {
			accountID = normalizeCodexRegistrationIdentity(accounts[i].GetCredential("account_id"))
		}
		if accountID != "" {
			index.accountIDs[accountID] = struct{}{}
		}
		email := normalizeCodexRegistrationIdentity(accounts[i].GetCredential("email"))
		if email != "" {
			index.emails[email] = struct{}{}
		}
	}
	return index
}

func applyCodexRegistrationExistingAccountDuplicates(
	candidates map[string]CodexRegistrationCandidateState,
	index codexRegistrationDuplicateIndex,
	updatedAt time.Time,
) {
	for sourcePath, candidate := range candidates {
		if isCodexRegistrationDuplicate(candidate, index) {
			candidate.WorkflowState = CodexRegistrationWorkflowDuplicate
			candidate.UpdatedAt = updatedAt
			candidates[sourcePath] = candidate
		}
	}
}

func isCodexRegistrationDuplicate(candidate CodexRegistrationCandidateState, index codexRegistrationDuplicateIndex) bool {
	if accountID := normalizeCodexRegistrationIdentity(candidate.AccountID); accountID != "" {
		if _, exists := index.accountIDs[accountID]; exists {
			return true
		}
	}
	if email := normalizeCodexRegistrationIdentity(candidate.Email); email != "" {
		if _, exists := index.emails[email]; exists {
			return true
		}
	}
	return false
}

func normalizeCodexRegistrationIdentity(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func readStringFromMap(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		raw, ok := payload[key]
		if !ok || raw == nil {
			continue
		}
		switch value := raw.(type) {
		case string:
			return value
		case json.Number:
			return value.String()
		case float64:
			return strconv.FormatFloat(value, 'f', -1, 64)
		case int64:
			return strconv.FormatInt(value, 10)
		case int:
			return strconv.Itoa(value)
		}
	}
	return ""
}

func readTimeFromMap(payload map[string]any, keys ...string) *time.Time {
	for _, key := range keys {
		raw, ok := payload[key]
		if !ok || raw == nil {
			continue
		}
		parsed := parseCodexRegistrationTime(raw)
		if parsed != nil {
			return parsed
		}
	}
	return nil
}

func parseCodexRegistrationTime(raw any) *time.Time {
	switch value := raw.(type) {
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return nil
		}
		if ts, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
			t := time.Unix(ts, 0).UTC()
			return &t
		}
		if t, err := time.Parse(time.RFC3339, trimmed); err == nil {
			utc := t.UTC()
			return &utc
		}
	case json.Number:
		if ts, err := value.Int64(); err == nil {
			t := time.Unix(ts, 0).UTC()
			return &t
		}
	case float64:
		t := time.Unix(int64(value), 0).UTC()
		return &t
	case int64:
		t := time.Unix(value, 0).UTC()
		return &t
	case int:
		t := time.Unix(int64(value), 0).UTC()
		return &t
	}
	return nil
}

func isCodexRegistrationAuthFailure(err error) bool {
	if err == nil {
		return false
	}
	code := infraerrors.Code(err)
	if code == http.StatusUnauthorized {
		return true
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "status 401") || strings.Contains(msg, "status=401") {
		return true
	}
	return hasCodexRegistrationAuthMarker(msg)
}

func hasCodexRegistrationAuthMarker(text string) bool {
	for _, marker := range codexRegistrationAuthFailureMarkers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func isCodexRegistrationTimeout(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return false
}

func uniqueInt64(values []int64) []int64 {
	seen := make(map[int64]struct{}, len(values))
	unique := make([]int64, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	return unique
}
