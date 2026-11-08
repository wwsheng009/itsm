package bootstrap

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
)

// postSchemaConstraintsSQL contains constraints and column defaults that ent
// cannot model (notably composite tenant-safe foreign keys). Ent's Schema.Create
// rebuilds tables on startup and drops unknown constraints, so this idempotent
// hardening step must run after every schema creation, not only once through the
// versioned migration ledger.
//
//go:embed post_schema_constraints.sql
var postSchemaConstraintsSQL string

func applyPostSchemaConstraints(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return nil
	}
	if _, err := db.ExecContext(ctx, postSchemaConstraintsSQL); err != nil {
		return fmt.Errorf("execute post-schema constraints: %w", err)
	}
	return nil
}
