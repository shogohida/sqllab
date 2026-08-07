package session

import (
	"testing"
	"time"

	sqllabdb "sqllab/internal/db"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	path, err := sqllabdb.BuildTemplate(t.TempDir())
	if err != nil {
		t.Fatalf("build template: %v", err)
	}
	return NewStore(path)
}

func TestCreate_SeedsAndRegisters(t *testing.T) {
	store := testStore(t)

	sess, err := store.Create()
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sess.Token == "" {
		t.Fatal("expected a non-empty token")
	}

	var n int
	if err := sess.DB.QueryRow("SELECT COUNT(*) FROM customers").Scan(&n); err != nil {
		t.Fatalf("query seeded db: %v", err)
	}
	if n == 0 {
		t.Fatal("expected the session db to be seeded with customers")
	}

	if got, ok := store.Get(sess.Token); !ok || got.Token != sess.Token {
		t.Fatalf("Get did not return the created session")
	}
	if store.Len() != 1 {
		t.Fatalf("expected 1 live session, got %d", store.Len())
	}
}

func TestGet_UnknownToken(t *testing.T) {
	store := testStore(t)
	if _, ok := store.Get("does-not-exist"); ok {
		t.Fatal("expected unknown token to miss")
	}
}

func TestCreate_EnforcesCapacity(t *testing.T) {
	store := testStore(t)

	for i := 0; i < MaxSessions; i++ {
		if _, err := store.Create(); err != nil {
			t.Fatalf("session %d: unexpected error: %v", i, err)
		}
	}

	if _, err := store.Create(); err != ErrAtCapacity {
		t.Fatalf("expected ErrAtCapacity once at the cap, got: %v", err)
	}
}

func TestEvictIdle_RemovesOnlyStaleSessions(t *testing.T) {
	store := testStore(t)

	fresh, err := store.Create()
	if err != nil {
		t.Fatalf("Create fresh: %v", err)
	}
	stale, err := store.Create()
	if err != nil {
		t.Fatalf("Create stale: %v", err)
	}

	store.mu.Lock()
	store.sessions[stale.Token].LastAccessed = time.Now().Add(-idleTTL - time.Minute)
	store.mu.Unlock()

	store.evictIdle()

	if _, ok := store.Get(fresh.Token); !ok {
		t.Fatal("fresh session should not have been evicted")
	}
	if _, ok := store.Get(stale.Token); ok {
		t.Fatal("stale session should have been evicted")
	}
}
