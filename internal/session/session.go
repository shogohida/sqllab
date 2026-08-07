// Package session gives every visitor their own ephemeral, isolated
// in-memory SQLite database so one visitor's index/schema experiments can
// never affect another's, and there is no persistent state to corrupt or
// grow unbounded across restarts.
package session

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

const CookieName = "sqllab_session"

// Session pairs a token with its own SQLite connection. The connection
// pool is capped at one open connection (see store.go's newSessionDB) so
// ":memory:" state stays visible across calls without needing the
// cache=shared DSN — SQLite serializes writes anyway, so a single
// connection costs nothing in practice for this workload.
type Session struct {
	Token        string
	DB           *sql.DB
	CreatedAt    time.Time
	LastAccessed time.Time
}

func newToken() (string, error) {
	b := make([]byte, 16) // 128 bits
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}
	return hex.EncodeToString(b), nil
}
