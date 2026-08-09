package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sqllabdb "sqllab/internal/db"
	"sqllab/internal/session"
)

func testHandler(t *testing.T) *Handler {
	t.Helper()
	path, err := sqllabdb.BuildTemplate(t.TempDir())
	if err != nil {
		t.Fatalf("build template: %v", err)
	}
	return New(session.NewStore(path))
}

func postQuery(t *testing.T, srv *httptest.Server, cookies []*http.Cookie, sql string) (*http.Response, sqllabdb.Result) {
	t.Helper()
	body, _ := json.Marshal(queryRequest{SQL: sql})
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/query", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("POST /api/query: %v", err)
	}
	var result sqllabdb.Result
	json.NewDecoder(resp.Body).Decode(&result)
	resp.Body.Close()
	return resp, result
}

func TestHandleQuery_AutoProvisionsSessionAndPersistsAcrossRequests(t *testing.T) {
	srv := httptest.NewServer(testHandler(t).Routes())
	defer srv.Close()

	resp1, res1 := postQuery(t, srv, nil, "SELECT * FROM customers LIMIT 1")
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("first query: expected 200, got %d", resp1.StatusCode)
	}
	if res1.RowCount != 1 {
		t.Fatalf("expected 1 row, got %d", res1.RowCount)
	}
	if len(resp1.Cookies()) == 0 {
		t.Fatal("expected a session cookie to be set")
	}

	// Second request without the cookie gets its own fresh session and
	// still works — sessions aren't required to already exist.
	resp2, _ := postQuery(t, srv, nil, "SELECT * FROM products LIMIT 1")
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("second query (no cookie): expected 200, got %d", resp2.StatusCode)
	}

	// Third request reusing the first cookie hits the same session.
	resp3, res3 := postQuery(t, srv, resp1.Cookies(), "CREATE INDEX idx_test ON customers(city)")
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("indexed create on existing session: expected 200, got %d: %v", resp3.StatusCode, res3)
	}
	if res3.Kind != "create_index" {
		t.Fatalf("expected kind create_index, got %q", res3.Kind)
	}
}

func TestHandleQuery_RejectsDisallowedStatement(t *testing.T) {
	srv := httptest.NewServer(testHandler(t).Routes())
	defer srv.Close()

	resp, _ := postQuery(t, srv, nil, "DROP TABLE customers")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for a disallowed statement, got %d", resp.StatusCode)
	}
}

func TestHandleSuggestIndex(t *testing.T) {
	srv := httptest.NewServer(testHandler(t).Routes())
	defer srv.Close()

	postSuggest := func(sql string) (*http.Response, sqllabdb.IndexSuggestion) {
		t.Helper()
		body, _ := json.Marshal(suggestIndexRequest{SQL: sql})
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/suggest-index", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatalf("POST /api/suggest-index: %v", err)
		}
		var out sqllabdb.IndexSuggestion
		json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		return resp, out
	}

	resp, sugg := postSuggest("SELECT * FROM orders WHERE customer_id = 42 ORDER BY order_date DESC")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if sugg.SQL == "" {
		t.Fatalf("expected a suggested index, got reason %q", sugg.Reason)
	}

	// Getting a suggestion must not itself create the index: a normal query
	// against the same (cookie-bound) session should still show a scan.
	cookies := resp.Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected a session cookie to be set")
	}
	respPlan, result := postQuery(t, srv, cookies, "EXPLAIN QUERY PLAN SELECT * FROM orders WHERE customer_id = 42 ORDER BY order_date DESC")
	if respPlan.StatusCode != http.StatusOK {
		t.Fatalf("EXPLAIN QUERY PLAN: expected 200, got %d", respPlan.StatusCode)
	}
	found := false
	for _, line := range result.Plan {
		if strings.HasPrefix(line, "SCAN") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the suggestion call to leave the session unindexed (a plain SCAN), got plan %v", result.Plan)
	}

	respBad, _ := postSuggest("CREATE INDEX idx ON orders(status)")
	if respBad.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for a non-SELECT statement, got %d", respBad.StatusCode)
	}
}

func TestHandleSchemaAndScenarios(t *testing.T) {
	srv := httptest.NewServer(testHandler(t).Routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/schema")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/schema: err=%v status=%v", err, resp)
	}
	var tables []sqllabdb.Table
	json.NewDecoder(resp.Body).Decode(&tables)
	if len(tables) != len(sqllabdb.Describe()) {
		t.Fatalf("expected %d tables, got %d", len(sqllabdb.Describe()), len(tables))
	}

	resp2, err := http.Get(srv.URL + "/api/scenarios")
	if err != nil || resp2.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/scenarios: err=%v status=%v", err, resp2)
	}
}
