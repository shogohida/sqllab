package scenarios

import (
	"testing"

	sqllabdb "sqllab/internal/db"
)

// Every scenario's query and suggested index must pass the same guard the
// public API enforces — a scenario that the guard would reject is a bug in
// the scenario definition, not a guard problem.
func TestScenarios_PassTheGuard(t *testing.T) {
	for _, s := range All {
		if _, _, err := sqllabdb.ValidateStatement(s.Query); err != nil {
			t.Errorf("scenario %q: query rejected by guard: %v", s.ID, err)
		}
		if _, _, err := sqllabdb.ValidateStatement(s.SuggestedIndexSQL); err != nil {
			t.Errorf("scenario %q: suggested index rejected by guard: %v", s.ID, err)
		}
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
