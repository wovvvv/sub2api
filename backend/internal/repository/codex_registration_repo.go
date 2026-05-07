package repository

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/codexregistrationcandidate"
	"github.com/Wei-Shaw/sub2api/ent/predicate"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type codexRegistrationCandidateRepository struct {
	client *ent.Client
}

func NewCodexRegistrationCandidateRepository(client *ent.Client) service.CodexRegistrationCandidateRepository {
	return &codexRegistrationCandidateRepository{client: client}
}

func (r *codexRegistrationCandidateRepository) List(ctx context.Context) ([]service.CodexRegistrationCandidateState, error) {
	rows, err := r.client.CodexRegistrationCandidate.Query().
		Order(ent.Asc(codexregistrationcandidate.FieldID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]service.CodexRegistrationCandidateState, 0, len(rows))
	for i := range rows {
		out = append(out, codexRegistrationCandidateToState(rows[i]))
	}
	return out, nil
}

func (r *codexRegistrationCandidateRepository) GetByID(ctx context.Context, id int64) (*service.CodexRegistrationCandidateState, error) {
	row, err := r.client.CodexRegistrationCandidate.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	state := codexRegistrationCandidateToState(row)
	return &state, nil
}

func (r *codexRegistrationCandidateRepository) ListByIDs(ctx context.Context, ids []int64) ([]service.CodexRegistrationCandidateState, error) {
	if len(ids) == 0 {
		return []service.CodexRegistrationCandidateState{}, nil
	}
	rows, err := r.client.CodexRegistrationCandidate.Query().
		Where(codexregistrationcandidate.IDIn(ids...)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]service.CodexRegistrationCandidateState, 0, len(rows))
	for i := range rows {
		out = append(out, codexRegistrationCandidateToState(rows[i]))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (r *codexRegistrationCandidateRepository) UpsertBySourcePath(ctx context.Context, candidate service.CodexRegistrationCandidateState) (*service.CodexRegistrationCandidateState, error) {
	existing, err := r.client.CodexRegistrationCandidate.Query().
		Where(codexregistrationcandidate.SourcePathEQ(candidate.SourcePath)).
		Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return nil, err
	}

	if existing == nil {
		builder := r.client.CodexRegistrationCandidate.Create().
			SetSourcePath(candidate.SourcePath).
			SetSourceFilename(candidate.SourceFilename).
			SetSourceMtime(candidate.SourceMtime).
			SetSourceFingerprint(candidate.SourceFingerprint).
			SetLivenessStatus(string(candidate.LivenessStatus)).
			SetWorkflowState(string(candidate.WorkflowState))
		applyCodexRegistrationCandidateCreateMutableFields(builder, candidate)
		if !candidate.CreatedAt.IsZero() {
			builder.SetCreatedAt(candidate.CreatedAt)
		}
		if !candidate.UpdatedAt.IsZero() {
			builder.SetUpdatedAt(candidate.UpdatedAt)
		}
		saved, saveErr := builder.Save(ctx)
		if saveErr != nil {
			return nil, saveErr
		}
		state := codexRegistrationCandidateToState(saved)
		return &state, nil
	}

	if shouldPreserveCurrentCandidate(existing, candidate) {
		state := codexRegistrationCandidateToState(existing)
		return &state, nil
	}

	candidate = mergeStickyImportedState(existing, candidate)

	builder := r.client.CodexRegistrationCandidate.Update().
		Where(
			codexregistrationcandidate.IDEQ(existing.ID),
			codexregistrationcandidate.UpdatedAtEQ(existing.UpdatedAt),
			codexregistrationcandidate.WorkflowStateEQ(existing.WorkflowState),
			codexregistrationcandidate.LivenessStatusEQ(existing.LivenessStatus),
			codexregistrationcandidate.SourceFingerprintEQ(existing.SourceFingerprint),
		).
		SetSourceFilename(candidate.SourceFilename).
		SetSourceMtime(candidate.SourceMtime).
		SetSourceFingerprint(candidate.SourceFingerprint).
		SetLivenessStatus(string(candidate.LivenessStatus)).
		SetWorkflowState(string(candidate.WorkflowState))
	applyCodexRegistrationCandidateUpdateMutableFieldsForBatch(builder, candidate)
	if !candidate.UpdatedAt.IsZero() {
		builder.SetUpdatedAt(candidate.UpdatedAt)
	}
	affected, saveErr := builder.Save(ctx)
	if saveErr != nil {
		return nil, saveErr
	}
	if affected != 1 {
		return nil, fmt.Errorf("candidate %d changed during upsert", existing.ID)
	}
	saved, getErr := r.client.CodexRegistrationCandidate.Get(ctx, existing.ID)
	if getErr != nil {
		return nil, getErr
	}
	state := codexRegistrationCandidateToState(saved)
	return &state, nil
}

func (r *codexRegistrationCandidateRepository) Update(ctx context.Context, candidate service.CodexRegistrationCandidateState, expected *service.CodexRegistrationCandidateState) (*service.CodexRegistrationCandidateState, error) {
	if candidate.ID <= 0 {
		return nil, fmt.Errorf("candidate id is required")
	}
	current, err := r.client.CodexRegistrationCandidate.Get(ctx, candidate.ID)
	if err != nil {
		return nil, err
	}
	candidate = mergeStickyImportedState(current, candidate)
	guard := currentSnapshotForCandidate(current)
	if expected != nil {
		guard = *expected
	}
	predicates := codexRegistrationSnapshotPredicates(candidate.ID, guard, candidate.WorkflowState == service.CodexRegistrationWorkflowImported)
	builder := r.client.CodexRegistrationCandidate.Update().
		Where(predicates...).
		SetSourcePath(candidate.SourcePath).
		SetSourceFilename(candidate.SourceFilename).
		SetSourceMtime(candidate.SourceMtime).
		SetSourceFingerprint(candidate.SourceFingerprint).
		SetLivenessStatus(string(candidate.LivenessStatus)).
		SetWorkflowState(string(candidate.WorkflowState))
	applyCodexRegistrationCandidateUpdateMutableFieldsForBatch(builder, candidate)
	if !candidate.UpdatedAt.IsZero() {
		builder.SetUpdatedAt(candidate.UpdatedAt)
	}
	affected, err := builder.Save(ctx)
	if err != nil {
		return nil, err
	}
	if affected != 1 {
		return nil, fmt.Errorf("candidate %d changed during update", candidate.ID)
	}
	saved, err := r.client.CodexRegistrationCandidate.Get(ctx, candidate.ID)
	if err != nil {
		return nil, err
	}
	state := codexRegistrationCandidateToState(saved)
	return &state, nil
}

func (r *codexRegistrationCandidateRepository) UpdateBatch(ctx context.Context, candidates []service.CodexRegistrationCandidateState, expected map[int64]service.CodexRegistrationCandidateState) error {
	if len(candidates) == 0 {
		return nil
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	for _, candidate := range candidates {
		if candidate.ID <= 0 {
			return fmt.Errorf("candidate id is required")
		}
		current, getErr := tx.CodexRegistrationCandidate.Get(ctx, candidate.ID)
		if getErr != nil {
			return getErr
		}
		expectedCandidate, ok := expected[candidate.ID]
		if !ok {
			return fmt.Errorf("expected candidate state missing for id %d", candidate.ID)
		}
		if !sameCodexRegistrationSnapshot(current, expectedCandidate) {
			return fmt.Errorf("candidate %d changed during update", candidate.ID)
		}
		candidate = mergeStickyImportedState(current, candidate)

		builder := tx.CodexRegistrationCandidate.Update().
			Where(
				codexregistrationcandidate.IDEQ(candidate.ID),
				codexregistrationcandidate.UpdatedAtEQ(current.UpdatedAt),
				codexregistrationcandidate.WorkflowStateEQ(current.WorkflowState),
				codexregistrationcandidate.LivenessStatusEQ(current.LivenessStatus),
				codexregistrationcandidate.SourceFingerprintEQ(current.SourceFingerprint),
			).
			SetSourcePath(candidate.SourcePath).
			SetSourceFilename(candidate.SourceFilename).
			SetSourceMtime(candidate.SourceMtime).
			SetSourceFingerprint(candidate.SourceFingerprint).
			SetLivenessStatus(string(candidate.LivenessStatus)).
			SetWorkflowState(string(candidate.WorkflowState))
		applyCodexRegistrationCandidateUpdateMutableFieldsForBatch(builder, candidate)
		if !candidate.UpdatedAt.IsZero() {
			builder.SetUpdatedAt(candidate.UpdatedAt)
		}
		affected, saveErr := builder.Save(ctx)
		if saveErr != nil {
			return saveErr
		}
		if affected != 1 {
			return fmt.Errorf("candidate %d changed during batch update", candidate.ID)
		}
	}

	if err = tx.Commit(); err != nil {
		return err
	}
	tx = nil
	return nil
}

func (r *codexRegistrationCandidateRepository) DeleteNonImportedBySourcePathsNotIn(ctx context.Context, sourcePaths []string, checkedAt time.Time) error {
	query := r.client.CodexRegistrationCandidate.Delete().
		Where(
			codexregistrationcandidate.WorkflowStateNEQ(string(service.CodexRegistrationWorkflowImported)),
			codexregistrationcandidate.UpdatedAtLTE(checkedAt),
		)
	if len(sourcePaths) > 0 {
		query = query.Where(codexregistrationcandidate.SourcePathNotIn(sourcePaths...))
	}
	_, err := query.Exec(ctx)
	return err
}

func (r *codexRegistrationCandidateRepository) DeleteAll(ctx context.Context) (int, error) {
	return r.client.CodexRegistrationCandidate.Delete().Exec(ctx)
}

func applyCodexRegistrationCandidateCreateMutableFields(
	builder *ent.CodexRegistrationCandidateCreate,
	candidate service.CodexRegistrationCandidateState,
) {
	if candidate.Email != "" {
		builder.SetEmail(candidate.Email)
	}
	if candidate.AccountID != "" {
		builder.SetAccountID(candidate.AccountID)
	}
	if candidate.Type != "" {
		builder.SetType(candidate.Type)
	}
	if candidate.ExpiresAt != nil {
		builder.SetExpiresAt(*candidate.ExpiresAt)
	}
	if candidate.LastRefreshAt != nil {
		builder.SetLastRefreshAt(*candidate.LastRefreshAt)
	}
	if candidate.StatusReason != "" {
		builder.SetStatusReason(candidate.StatusReason)
	}
	if !candidate.LastCheckedAt.IsZero() {
		builder.SetLastCheckedAt(candidate.LastCheckedAt)
	}
	if candidate.ImportedAccountID != nil {
		builder.SetImportedAccountID(*candidate.ImportedAccountID)
	}
	if candidate.ImportedAt != nil {
		builder.SetImportedAt(*candidate.ImportedAt)
	}
}

func applyCodexRegistrationCandidateUpdateMutableFields(
	builder *ent.CodexRegistrationCandidateUpdateOne,
	candidate service.CodexRegistrationCandidateState,
) {
	if candidate.Email != "" {
		builder.SetEmail(candidate.Email)
	} else {
		builder.ClearEmail()
	}
	if candidate.AccountID != "" {
		builder.SetAccountID(candidate.AccountID)
	} else {
		builder.ClearAccountID()
	}
	if candidate.Type != "" {
		builder.SetType(candidate.Type)
	} else {
		builder.ClearType()
	}
	if candidate.ExpiresAt != nil {
		builder.SetExpiresAt(*candidate.ExpiresAt)
	} else {
		builder.ClearExpiresAt()
	}
	if candidate.LastRefreshAt != nil {
		builder.SetLastRefreshAt(*candidate.LastRefreshAt)
	} else {
		builder.ClearLastRefreshAt()
	}
	if candidate.StatusReason != "" {
		builder.SetStatusReason(candidate.StatusReason)
	} else {
		builder.ClearStatusReason()
	}
	if !candidate.LastCheckedAt.IsZero() {
		builder.SetLastCheckedAt(candidate.LastCheckedAt)
	} else {
		builder.ClearLastCheckedAt()
	}
	if candidate.ImportedAccountID != nil {
		builder.SetImportedAccountID(*candidate.ImportedAccountID)
	} else {
		builder.ClearImportedAccountID()
	}
	if candidate.ImportedAt != nil {
		builder.SetImportedAt(*candidate.ImportedAt)
	} else {
		builder.ClearImportedAt()
	}
}

func applyCodexRegistrationCandidateUpdateMutableFieldsForBatch(
	builder *ent.CodexRegistrationCandidateUpdate,
	candidate service.CodexRegistrationCandidateState,
) {
	if candidate.Email != "" {
		builder.SetEmail(candidate.Email)
	} else {
		builder.ClearEmail()
	}
	if candidate.AccountID != "" {
		builder.SetAccountID(candidate.AccountID)
	} else {
		builder.ClearAccountID()
	}
	if candidate.Type != "" {
		builder.SetType(candidate.Type)
	} else {
		builder.ClearType()
	}
	if candidate.ExpiresAt != nil {
		builder.SetExpiresAt(*candidate.ExpiresAt)
	} else {
		builder.ClearExpiresAt()
	}
	if candidate.LastRefreshAt != nil {
		builder.SetLastRefreshAt(*candidate.LastRefreshAt)
	} else {
		builder.ClearLastRefreshAt()
	}
	if candidate.StatusReason != "" {
		builder.SetStatusReason(candidate.StatusReason)
	} else {
		builder.ClearStatusReason()
	}
	if !candidate.LastCheckedAt.IsZero() {
		builder.SetLastCheckedAt(candidate.LastCheckedAt)
	} else {
		builder.ClearLastCheckedAt()
	}
	if candidate.ImportedAccountID != nil {
		builder.SetImportedAccountID(*candidate.ImportedAccountID)
	} else {
		builder.ClearImportedAccountID()
	}
	if candidate.ImportedAt != nil {
		builder.SetImportedAt(*candidate.ImportedAt)
	} else {
		builder.ClearImportedAt()
	}
}

func codexRegistrationCandidateToState(row *ent.CodexRegistrationCandidate) service.CodexRegistrationCandidateState {
	state := service.CodexRegistrationCandidateState{
		ID:                row.ID,
		SourcePath:        row.SourcePath,
		SourceFilename:    row.SourceFilename,
		SourceMtime:       row.SourceMtime.UTC(),
		SourceFingerprint: row.SourceFingerprint,
		LivenessStatus:    service.CodexRegistrationLivenessStatus(row.LivenessStatus),
		WorkflowState:     service.CodexRegistrationWorkflowState(row.WorkflowState),
		LastCheckedAt:     nullableTimeToValue(row.LastCheckedAt),
		CreatedAt:         row.CreatedAt.UTC(),
		UpdatedAt:         row.UpdatedAt.UTC(),
	}
	if row.Email != nil {
		state.Email = *row.Email
	}
	if row.AccountID != nil {
		state.AccountID = *row.AccountID
	}
	if row.Type != nil {
		state.Type = *row.Type
	}
	if row.ExpiresAt != nil {
		expires := row.ExpiresAt.UTC()
		state.ExpiresAt = &expires
	}
	if row.LastRefreshAt != nil {
		lastRefresh := row.LastRefreshAt.UTC()
		state.LastRefreshAt = &lastRefresh
	}
	if row.StatusReason != nil {
		state.StatusReason = *row.StatusReason
	}
	if row.ImportedAccountID != nil {
		importedAccountID := *row.ImportedAccountID
		state.ImportedAccountID = &importedAccountID
	}
	if row.ImportedAt != nil {
		importedAt := row.ImportedAt.UTC()
		state.ImportedAt = &importedAt
	}
	return state
}

func mergeStickyImportedState(current *ent.CodexRegistrationCandidate, candidate service.CodexRegistrationCandidateState) service.CodexRegistrationCandidateState {
	if current == nil {
		return candidate
	}
	if current.WorkflowState == string(service.CodexRegistrationWorkflowImported) &&
		candidate.WorkflowState != service.CodexRegistrationWorkflowImported {
		currentState := codexRegistrationCandidateToState(current)
		currentState.ID = candidate.ID
		return currentState
	}
	if candidate.ImportedAccountID == nil && current.ImportedAccountID != nil {
		importedAccountID := *current.ImportedAccountID
		candidate.ImportedAccountID = &importedAccountID
	}
	if candidate.ImportedAt == nil && current.ImportedAt != nil {
		importedAt := current.ImportedAt.UTC()
		candidate.ImportedAt = &importedAt
	}
	return candidate
}

func sameCodexRegistrationSnapshot(current *ent.CodexRegistrationCandidate, expected service.CodexRegistrationCandidateState) bool {
	if current == nil {
		return false
	}
	if current.WorkflowState != string(expected.WorkflowState) {
		return false
	}
	if current.LivenessStatus != string(expected.LivenessStatus) {
		return false
	}
	if current.SourceFingerprint != expected.SourceFingerprint {
		return false
	}
	return current.UpdatedAt.UTC().Equal(expected.UpdatedAt.UTC())
}

func shouldPreserveCurrentCandidate(current *ent.CodexRegistrationCandidate, incoming service.CodexRegistrationCandidateState) bool {
	if current == nil {
		return false
	}
	if current.WorkflowState == string(service.CodexRegistrationWorkflowImported) &&
		incoming.WorkflowState != service.CodexRegistrationWorkflowImported {
		return true
	}
	if !incoming.LastCheckedAt.IsZero() && current.UpdatedAt.UTC().After(incoming.LastCheckedAt.UTC()) {
		return true
	}
	return false
}

func currentSnapshotForCandidate(current *ent.CodexRegistrationCandidate) service.CodexRegistrationCandidateState {
	snapshot := service.CodexRegistrationCandidateState{}
	if current == nil {
		return snapshot
	}
	snapshot.WorkflowState = service.CodexRegistrationWorkflowState(current.WorkflowState)
	snapshot.LivenessStatus = service.CodexRegistrationLivenessStatus(current.LivenessStatus)
	snapshot.SourceFingerprint = current.SourceFingerprint
	snapshot.UpdatedAt = current.UpdatedAt.UTC()
	return snapshot
}

func codexRegistrationSnapshotPredicates(id int64, expected service.CodexRegistrationCandidateState, allowBenignRefresh bool) []predicate.CodexRegistrationCandidate {
	predicates := []predicate.CodexRegistrationCandidate{
		codexregistrationcandidate.IDEQ(id),
		codexregistrationcandidate.WorkflowStateEQ(string(expected.WorkflowState)),
		codexregistrationcandidate.LivenessStatusEQ(string(expected.LivenessStatus)),
		codexregistrationcandidate.SourceFingerprintEQ(expected.SourceFingerprint),
	}
	if !allowBenignRefresh {
		predicates = append(predicates, codexregistrationcandidate.UpdatedAtEQ(expected.UpdatedAt))
	}
	return predicates
}

func nullableTimeToValue(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return t.UTC()
}
