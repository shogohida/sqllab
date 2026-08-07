package session

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	sqllabdb "sqllab/internal/db"

	_ "modernc.org/sqlite"
)

const (
	// MaxSessions bounds concurrent in-memory dataset copies. Render's free
	// tier is 512MB RAM shared with the Go runtime; each seeded session is
	// a few MB, so this cap leaves comfortable headroom rather than
	// silently evicting someone else's live demo under load.
	MaxSessions = 20
	idleTTL     = 10 * time.Minute
	sweepEvery  = 60 * time.Second
)

// ErrAtCapacity is returned by Create when MaxSessions concurrent sessions
// are already live. Callers should surface this as HTTP 503.
var ErrAtCapacity = fmt.Errorf("sqllab: at capacity, try again shortly")

// Store owns every live session's database and evicts idle ones.
type Store struct {
	templatePath string

	mu       sync.Mutex
	sessions map[string]*Session
}

func NewStore(templatePath string) *Store {
	return &Store{
		templatePath: templatePath,
		sessions:     make(map[string]*Session),
	}
}

// Create seeds a fresh in-memory database and registers a new session for
// it. Returns ErrAtCapacity if MaxSessions are already live.
func (s *Store) Create() (*Session, error) {
	s.mu.Lock()
	if len(s.sessions) >= MaxSessions {
		s.mu.Unlock()
		return nil, ErrAtCapacity
	}
	s.mu.Unlock()

	token, err := newToken()
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, fmt.Errorf("open session db: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := sqllabdb.Seed(db, s.templatePath); err != nil {
		db.Close()
		return nil, fmt.Errorf("seed session db: %w", err)
	}

	now := time.Now()
	sess := &Session{Token: token, DB: db, CreatedAt: now, LastAccessed: now}

	s.mu.Lock()
	defer s.mu.Unlock()
	// Re-check under lock: two concurrent Create calls could both pass the
	// capacity check above before either registers.
	if len(s.sessions) >= MaxSessions {
		db.Close()
		return nil, ErrAtCapacity
	}
	s.sessions[token] = sess
	return sess, nil
}

// Get looks up a session by token and refreshes its idle clock. The bool
// is false if the token is unknown or has been evicted.
func (s *Store) Get(token string) (*Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[token]
	if !ok {
		return nil, false
	}
	sess.LastAccessed = time.Now()
	return sess, true
}

// Len reports the current number of live sessions (used in tests and for
// an optional /api/health style endpoint).
func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}

// RunEvictionLoop blocks, sweeping idle sessions every sweepEvery until
// stop is closed. Run it in a goroutine from cmd/server.
func (s *Store) RunEvictionLoop(stop <-chan struct{}) {
	ticker := time.NewTicker(sweepEvery)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			s.evictIdle()
		}
	}
}

func (s *Store) evictIdle() {
	cutoff := time.Now().Add(-idleTTL)

	s.mu.Lock()
	defer s.mu.Unlock()
	for token, sess := range s.sessions {
		if sess.LastAccessed.Before(cutoff) {
			sess.DB.Close()
			delete(s.sessions, token)
		}
	}
}
