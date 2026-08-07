// Package api wires the HTTP surface: schema/scenario metadata (static)
// and the single query endpoint that every hand-typed or AI-generated SQL
// statement runs through against the caller's own session database.
package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	sqllabdb "sqllab/internal/db"
	"sqllab/internal/scenarios"
	"sqllab/internal/session"
)

type Handler struct {
	store *session.Store
}

func New(store *session.Store) *Handler {
	return &Handler{store: store}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/schema", h.handleSchema)
	mux.HandleFunc("GET /api/scenarios", h.handleScenarios)
	mux.HandleFunc("POST /api/query", h.handleQuery)
	return mux
}

func (h *Handler) handleSchema(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, sqllabdb.Describe())
}

func (h *Handler) handleScenarios(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, scenarios.All)
}

type queryRequest struct {
	SQL string `json:"sql"`
}

// handleQuery is the single entry point for every visitor-supplied or
// AI-generated statement — SELECT, EXPLAIN QUERY PLAN, CREATE INDEX, and
// DROP INDEX all flow through here and through the identical
// sqllabdb.Execute guard, whichever produced the SQL text.
func (h *Handler) handleQuery(w http.ResponseWriter, r *http.Request) {
	var req queryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	sess, err := h.sessionFor(w, r)
	if err != nil {
		if errors.Is(err, session.ErrAtCapacity) {
			writeError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		log.Printf("session error: %v", err)
		writeError(w, http.StatusInternalServerError, "could not create a session")
		return
	}

	result, err := sqllabdb.Execute(r.Context(), sess.DB, req.SQL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// sessionFor returns the caller's existing session (via cookie) or
// provisions a fresh one and sets the cookie, so the frontend never needs
// a separate "create session" round trip before its first query.
func (h *Handler) sessionFor(w http.ResponseWriter, r *http.Request) (*session.Session, error) {
	if c, err := r.Cookie(session.CookieName); err == nil {
		if sess, ok := h.store.Get(c.Value); ok {
			return sess, nil
		}
	}

	sess, err := h.store.Create()
	if err != nil {
		return nil, err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     session.CookieName,
		Value:    sess.Token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(24 * time.Hour),
	})
	return sess, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
