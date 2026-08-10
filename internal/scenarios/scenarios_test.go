package scenarios

import (
	"database/sql"
	"strings"
	"testing"

	sqllabdb "sqllab/internal/db"

	_ "modernc.org/sqlite"
)

// Every scenario's query and suggested index must pass the same guard the
// public API enforces — a scenario that the guard would reject is a bug in
// the scenario definition, not a guard problem.
func TestScenarios_PassTheGuard(t *testing.T) {
	for _, s := range All {
		if _, _, err := sqllabdb.ValidateStatement(s.Query); err != nil {
			t.Errorf("scenario %q: query rejected by guard: %v", s.ID, err)
		}
		for _, idxSQL := range s.SuggestedIndexSQL {
			if _, _, err := sqllabdb.ValidateStatement(idxSQL); err != nil {
				t.Errorf("scenario %q: suggested index %q rejected by guard: %v", s.ID, idxSQL, err)
			}
		}
		if s.RewrittenQuery != "" {
			if _, _, err := sqllabdb.ValidateStatement(s.RewrittenQuery); err != nil {
				t.Errorf("scenario %q: rewritten query rejected by guard: %v", s.ID, err)
			}
		}
		if len(s.SuggestedIndexSQL) == 0 && s.RewrittenQuery == "" {
			t.Errorf("scenario %q: has neither a suggested index nor a rewritten query", s.ID)
		}
	}
}

// The "orders-needing-review" scenario is the demo's one example of a query
// that genuinely needs two separate indexes — neither alone changes the
// plan for both of its EXISTS subqueries. That claim is easy to silently
// break (e.g. by changing seed row counts or re-enabling SQLite's
// automatic_index), so verify it directly against real data: applying only
// one of the two suggested indexes must leave at least one subquery table
// still showing a plain SCAN, and applying both must clear every SCAN.
func TestOrdersNeedingReview_TrulyNeedsBothIndexes(t *testing.T) {
	s, ok := ByID("orders-needing-review")
	if !ok {
		t.Fatal("scenario not found")
	}
	if len(s.SuggestedIndexSQL) < 2 {
		t.Fatalf("expected at least 2 suggested indexes, got %d", len(s.SuggestedIndexSQL))
	}

	planFor := func(t *testing.T, indexes []string) []string {
		t.Helper()
		templatePath, err := sqllabdb.BuildTemplate(t.TempDir())
		if err != nil {
			t.Fatalf("build template: %v", err)
		}
		db, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			t.Fatalf("open session db: %v", err)
		}
		db.SetMaxOpenConns(1)
		t.Cleanup(func() { db.Close() })
		if err := sqllabdb.Seed(db, templatePath); err != nil {
			t.Fatalf("seed session db: %v", err)
		}
		for _, idx := range indexes {
			if _, err := db.Exec(idx); err != nil {
				t.Fatalf("create index %q: %v", idx, err)
			}
		}
		rows, err := db.Query("EXPLAIN QUERY PLAN " + s.Query)
		if err != nil {
			t.Fatalf("explain: %v", err)
		}
		defer rows.Close()
		var plan []string
		for rows.Next() {
			var id, parent, notUsed int
			var detail string
			if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
				t.Fatalf("scan plan row: %v", err)
			}
			plan = append(plan, detail)
		}
		return plan
	}

	// The outer "orders" scan (SCAN o) is expected and fine in every
	// state — it's never the bottleneck here. Only a SCAN of the two
	// EXISTS subqueries' own tables (r, p) indicates the missing-index
	// problem this scenario is about.
	subqueryScanned := func(plan []string) bool {
		for _, line := range plan {
			upper := strings.ToUpper(line)
			if strings.HasPrefix(upper, "SCAN R") || strings.HasPrefix(upper, "SCAN P") {
				return true
			}
		}
		return false
	}

	if plan := planFor(t, nil); !subqueryScanned(plan) {
		t.Errorf("expected a subquery SCAN with no indexes at all, got plan %v", plan)
	}
	for i := range s.SuggestedIndexSQL {
		only := []string{s.SuggestedIndexSQL[i]}
		if plan := planFor(t, only); !subqueryScanned(plan) {
			t.Errorf("expected a subquery SCAN with only index %q applied, got plan %v", only[0], plan)
		}
	}
	if plan := planFor(t, s.SuggestedIndexSQL); subqueryScanned(plan) {
		t.Errorf("expected no subquery SCAN with both indexes applied, got plan %v", plan)
	}
}

func TestByID(t *testing.T) {
	if _, ok := ByID("customer-order-history"); !ok {
		t.Fatal("expected known scenario to be found")
	}
	if _, ok := ByID("nope"); ok {
		t.Fatal("expected unknown scenario id to miss")
	}
}
