package schema

import (
	"fmt"

	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// CodexRegistrationCandidate stores discovered host registration files and their workflow state.
type CodexRegistrationCandidate struct {
	ent.Schema
}

func (CodexRegistrationCandidate) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "codex_registration_candidates"},
	}
}

func (CodexRegistrationCandidate) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
	}
}

func (CodexRegistrationCandidate) Fields() []ent.Field {
	return []ent.Field{
		field.String("source_path").
			MaxLen(2048).
			NotEmpty().
			Unique(),
		field.String("source_filename").
			MaxLen(255).
			NotEmpty(),
		field.Time("source_mtime").
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.String("source_fingerprint").
			MaxLen(128).
			NotEmpty(),
		field.String("email").
			MaxLen(320).
			Optional().
			Nillable(),
		field.String("account_id").
			MaxLen(128).
			Optional().
			Nillable(),
		field.String("type").
			MaxLen(50).
			Optional().
			Nillable(),
		field.Time("expires_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("last_refresh_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.String("liveness_status").
			MaxLen(20).
			Default("error").
			Validate(validateCodexRegistrationCandidateLivenessStatus),
		field.String("workflow_state").
			MaxLen(20).
			Default("detected").
			Validate(validateCodexRegistrationCandidateWorkflowState),
		field.String("status_reason").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Time("last_checked_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Int64("imported_account_id").
			Optional().
			Nillable(),
		field.Time("imported_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (CodexRegistrationCandidate) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("workflow_state"),
		index.Fields("liveness_status"),
		index.Fields("account_id"),
	}
}

func validateCodexRegistrationCandidateLivenessStatus(status string) error {
	switch status {
	case "alive", "dead", "invalid", "error":
		return nil
	default:
		return fmt.Errorf("invalid codex registration candidate liveness_status: %s", status)
	}
}

func validateCodexRegistrationCandidateWorkflowState(state string) error {
	switch state {
	case "detected", "staged", "duplicate", "imported":
		return nil
	default:
		return fmt.Errorf("invalid codex registration candidate workflow_state: %s", state)
	}
}
