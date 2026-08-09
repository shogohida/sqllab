package db

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"time"
)

const (
	queryTimeout = 3 * time.Second
	maxRows      = 500
)

// explainPrefixRe strips a hand-typed "EXPLAIN QUERY PLAN" prefix so it can
// be re-added exactly once by explainPlan — without this, a KindExplain
// statement's own prefix would double up with explainPlan's.
var explainPrefixRe = regexp.MustCompile(`(?i)^EXPLAIN\s+QUERY\s+PLAN\s+`)

// Result is what every /api/query response carries back to the browser,
// whether the statement was a read query or an index DDL action.
type Result struct {
	Kind      string   `json:"kind"` // "select" | "explain" | "create_index" | "drop_index"
	Columns   []string `json:"columns,omitempty"`
	Rows      [][]any  `json:"rows,omitempty"`
	Plan      []string `json:"plan,omitempty"`
	ElapsedMs float64  `json:"elapsed_ms"`
	Truncated bool     `json:"truncated,omitempty"`
	RowCount  int      `json:"row_count,omitempty"`
}

// Execute validates stmt and runs it against db, applying a hard timeout
// and (for read queries) a row cap. It is the single entry point used for
// both hand-typed SQL and AI-generated SQL — see ValidateStatement's doc
// comment for why those are treated identically.
func Execute(ctx context.Context, sqlDB *sql.DB, stmt string) (*Result, error) {
	kind, clean, err := ValidateStatement(stmt)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	switch kind {
	case KindSelect:
		return runSelect(ctx, sqlDB, clean)
	case KindExplain:
		plan, elapsed, err := explainPlan(ctx, sqlDB, explainPrefixRe.ReplaceAllString(clean, ""))
		if err != nil {
			return nil, err
		}
		return &Result{Kind: "explain", Plan: plan, ElapsedMs: elapsed}, nil
	case KindCreateIndex:
		return runDDL(ctx, sqlDB, clean, "create_index")
	case KindDropIndex:
		return runDDL(ctx, sqlDB, clean, "drop_index")
	default:
		return nil, fmt.Errorf("unhandled statement kind")
	}
}

// runSelect executes the plan for clean first (cheap, informative even if
// the query itself is slow) and then the query itself, capping the
// returned rows so a visitor can't pull an unbounded result set through a
// free-tier instance.
func runSelect(ctx context.Context, sqlDB *sql.DB, clean string) (*Result, error) {
	plan, _, err := explainPlan(ctx, sqlDB, clean)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	rows, err := sqlDB.QueryContext(ctx, clean)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var out [][]any
	truncated := false
	for rows.Next() {
		if len(out) >= maxRows {
			truncated = true
			break
		}
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		out = append(out, vals)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	elapsed := time.Since(start).Seconds() * 1000

	return &Result{
		Kind:      "select",
		Columns:   cols,
		Rows:      out,
		Plan:      plan,
		ElapsedMs: elapsed,
		Truncated: truncated,
		RowCount:  len(out),
	}, nil
}

// queryer is satisfied by both *sql.DB and *sql.Tx, so explainPlan can run
// either directly against a session's connection or inside a trial
// transaction (see suggest.go) without duplicating this logic.
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func explainPlan(ctx context.Context, q queryer, selectSQL string) ([]string, float64, error) {
	start := time.Now()
	rows, err := q.QueryContext(ctx, "EXPLAIN QUERY PLAN "+selectSQL)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var plan []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			return nil, 0, err
		}
		plan = append(plan, detail)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return plan, time.Since(start).Seconds() * 1000, nil
}

func runDDL(ctx context.Context, sqlDB *sql.DB, clean, kind string) (*Result, error) {
	start := time.Now()
	if _, err := sqlDB.ExecContext(ctx, clean); err != nil {
		return nil, err
	}
	return &Result{Kind: kind, ElapsedMs: time.Since(start).Seconds() * 1000}, nil
}
