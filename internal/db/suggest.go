package db

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

// IndexSuggestion is the result of analyzing an arbitrary SELECT: either a
// CREATE INDEX statement that was empirically verified (via a rolled-back
// trial) to change the query's plan, or a Reason explaining why none was
// found — no index helps, or the blocking predicate wraps its column in a
// function call and needs a rewrite instead.
type IndexSuggestion struct {
	SQL    string `json:"suggested_index_sql,omitempty"`
	Reason string `json:"reason,omitempty"`
}

var (
	scanLineRe = regexp.MustCompile(`(?i)^SCAN\s+(?:TABLE\s+)?([A-Za-z_][A-Za-z0-9_]*)`)
	// planLineRe matches either a SCAN or a SEARCH line — used to find a
	// table's plan line whether or not it's currently using an index,
	// unlike scanLineRe which only finds the (unindexed) SCAN case.
	planLineRe = regexp.MustCompile(`(?i)^(?:SCAN|SEARCH)\s+(?:TABLE\s+)?([A-Za-z_][A-Za-z0-9_]*)`)
	// The symbol operators (=, <, ...) can't be followed by \b — "=" itself
	// is a non-word char, so the boundary check would fail whenever it's
	// followed by whitespace, which is the common case. Only LIKE/IN/BETWEEN
	// need \b, to avoid matching a longer identifier that merely starts with
	// one of those words.
	predicateRe  = regexp.MustCompile(`(?i)^\(*\s*(?:([A-Za-z_]\w*)\.)?([A-Za-z_]\w*)\s*(=|<=|>=|<|>|LIKE\b|IN\b|BETWEEN\b)`)
	joinEqRe     = regexp.MustCompile(`(?i)^\(*\s*(?:([A-Za-z_]\w*)\.)?([A-Za-z_]\w*)\s*=\s*(?:([A-Za-z_]\w*)\.)?([A-Za-z_]\w*)`)
	orderTermRe  = regexp.MustCompile(`(?i)^\s*(?:([A-Za-z_]\w*)\.)?([A-Za-z_]\w*)`)
	whereClause  = regexp.MustCompile(`(?is)\bWHERE\b(.*?)(\bGROUP\s+BY\b|\bORDER\s+BY\b|\bLIMIT\b|$)`)
	onClauses    = regexp.MustCompile(`(?is)\bON\b(.*?)(\bJOIN\b|\bWHERE\b|\bGROUP\s+BY\b|\bORDER\s+BY\b|\bLIMIT\b|$)`)
	orderByClaus = regexp.MustCompile(`(?is)\bORDER\s+BY\b(.*?)(\bLIMIT\b|$)`)
	reservedWord = map[string]bool{
		"ON": true, "WHERE": true, "JOIN": true, "GROUP": true, "ORDER": true,
		"LIMIT": true, "INNER": true, "LEFT": true, "RIGHT": true, "OUTER": true,
		"CROSS": true, "USING": true, "SET": true, "VALUES": true, "AS": true,
	}
)

// SuggestIndex analyzes a SELECT statement against sqlDB's current schema
// and, for each table SQLite has to scan, tries a candidate composite index
// built from that table's WHERE/JOIN/ORDER BY columns. Each candidate is
// created and tested inside a transaction that is always rolled back, so
// calling this never mutates sqlDB — it only ever reports what an index
// would do.
func SuggestIndex(ctx context.Context, sqlDB *sql.DB, stmt string) (*IndexSuggestion, error) {
	kind, clean, err := ValidateStatement(stmt)
	if err != nil {
		return nil, err
	}
	if kind != KindSelect {
		return nil, fmt.Errorf("only a SELECT statement can be analyzed for an index suggestion")
	}

	basePlan, _, err := explainPlan(ctx, sqlDB, clean)
	if err != nil {
		return nil, err
	}

	refs := tableRefs(clean)

	var lastReason string
	for _, table := range scannedTables(basePlan, refs) {
		alias := aliasFor(refs, table)
		cols, unsargableCol := candidateColumns(clean, table, alias)

		if len(cols) == 0 {
			if unsargableCol != "" {
				lastReason = fmt.Sprintf(
					"%s is scanned, but %s is wrapped in a function call in the query, so SQLite can't use an index through it — the query needs to be rewritten to a direct comparison first.",
					table, unsargableCol)
			} else {
				lastReason = fmt.Sprintf("%s is scanned, but no WHERE/JOIN/ORDER BY column referencing it was found to index.", table)
			}
			continue
		}

		candidateSQL := fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_auto_%s_%s ON %s(%s)",
			table, strings.Join(cols, "_"), table, strings.Join(cols, ", "))

		verified, err := verifyIndexHelps(ctx, sqlDB, candidateSQL, clean, basePlan, table, alias)
		if err != nil {
			return nil, err
		}
		if verified {
			return &IndexSuggestion{SQL: candidateSQL}, nil
		}
		lastReason = fmt.Sprintf("adding an index on %s(%s) didn't change the query plan for %s.", table, strings.Join(cols, ", "), table)
	}

	if lastReason == "" {
		lastReason = "this query doesn't scan any table without already using an index or its primary key."
	}
	return &IndexSuggestion{Reason: lastReason}, nil
}

// verifyIndexHelps creates candidateSQL and re-explains query inside a
// transaction that is unconditionally rolled back, then reports whether the
// plan line for table now mentions an index where the baseline plan didn't.
func verifyIndexHelps(ctx context.Context, sqlDB *sql.DB, candidateSQL, query string, basePlan []string, table, alias string) (bool, error) {
	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, candidateSQL); err != nil {
		return false, err
	}
	newPlan, _, err := explainPlan(ctx, tx, query)
	if err != nil {
		return false, err
	}

	before := planLineForTable(basePlan, table, alias)
	after := planLineForTable(newPlan, table, alias)
	usesIndexNow := after != "" && strings.Contains(strings.ToUpper(after), "INDEX")
	usedIndexBefore := before != "" && strings.Contains(strings.ToUpper(before), "INDEX")
	return usesIndexNow && !usedIndexBefore, nil
}

// scannedTables returns, in plan order, the distinct tables plan reports a
// SCAN against — the ones a candidate index could plausibly help. SQLite's
// plan output names a scan by the query's alias when the table has one, so
// each token is resolved back to its real table name through refs.
func scannedTables(plan []string, refs map[string]string) []string {
	var tables []string
	seen := map[string]bool{}
	for _, line := range plan {
		m := scanLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		table, ok := refs[strings.ToLower(m[1])]
		if !ok || seen[table] {
			continue
		}
		seen[table] = true
		tables = append(tables, table)
	}
	return tables
}

func planLineForTable(plan []string, table, alias string) string {
	for _, line := range plan {
		m := planLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if strings.EqualFold(m[1], table) || (alias != "" && strings.EqualFold(m[1], alias)) {
			return line
		}
	}
	return ""
}

// tableRefs maps every token a query uses to refer to one of the demo
// tables — the table name itself, plus its alias if the query's FROM/JOIN
// clause binds one (e.g. "FROM orders o" or "JOIN orders AS o") — back to
// that canonical table name.
func tableRefs(query string) map[string]string {
	refs := map[string]string{}
	for table := range AllowedColumns {
		re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(table) + `\b(?:\s+(?:AS\s+)?([A-Za-z_]\w*))?`)
		m := re.FindStringSubmatch(query)
		if m == nil {
			continue
		}
		refs[table] = table
		if m[1] != "" && !reservedWord[strings.ToUpper(m[1])] {
			refs[strings.ToLower(m[1])] = table
		}
	}
	return refs
}

func aliasFor(refs map[string]string, table string) string {
	for token, t := range refs {
		if t == table && token != table {
			return token
		}
	}
	return ""
}

// refersToTable reports whether a bare or prefixed column reference (prefix,
// col) belongs to table, given the alias the query binds it to.
func refersToTable(prefix, col, table, alias string, cols map[string]bool) bool {
	if prefix != "" {
		return strings.EqualFold(prefix, table) || (alias != "" && strings.EqualFold(prefix, alias))
	}
	return cols[strings.ToLower(col)]
}

// candidateColumns extracts an ordered, deduped list of table's columns
// worth indexing for query: equality (WHERE/JOIN) columns first, then at
// most one range column, then any remaining ORDER BY column. If a WHERE
// predicate wraps one of table's columns in a function call — making it
// unusable through an index no matter what — that column name is returned
// as unsargableCol instead.
func candidateColumns(query, table, alias string) (cols []string, unsargableCol string) {
	allowed := AllowedColumns[table]
	added := map[string]bool{}
	var equality, ranged, ordering []string

	add := func(list *[]string, col string) {
		col = strings.ToLower(col)
		if !allowed[col] || added[col] {
			return
		}
		added[col] = true
		*list = append(*list, col)
	}

	if wm := whereClause.FindStringSubmatch(query); wm != nil {
		for _, cond := range splitTopLevel(wm[1], "AND") {
			cond = strings.TrimSpace(cond)
			if cond == "" {
				continue
			}
			if m := predicateRe.FindStringSubmatch(cond); m != nil {
				prefix, col, op := m[1], m[2], strings.ToUpper(m[3])
				if refersToTable(prefix, col, table, alias, allowed) {
					if op == "=" || op == "IN" {
						add(&equality, col)
					} else {
						add(&ranged, col)
					}
				}
				continue
			}
			// Didn't match a plain "[alias.]col OP ..." predicate — check
			// whether it's a table column wrapped in a function call
			// instead, e.g. strftime('%Y-%m', order_date) = '2025-08'.
			if col := wrappedColumn(cond, table, allowed); col != "" && unsargableCol == "" {
				unsargableCol = col
			}
		}
	}

	if om := onClauses.FindAllStringSubmatch(query, -1); om != nil {
		for _, m := range om {
			for _, cond := range splitTopLevel(m[1], "AND") {
				jm := joinEqRe.FindStringSubmatch(strings.TrimSpace(cond))
				if jm == nil {
					continue
				}
				if refersToTable(jm[1], jm[2], table, alias, allowed) {
					add(&equality, jm[2])
				}
				if refersToTable(jm[3], jm[4], table, alias, allowed) {
					add(&equality, jm[4])
				}
			}
		}
	}

	if om := orderByClaus.FindStringSubmatch(query); om != nil {
		for _, term := range strings.Split(om[1], ",") {
			m := orderTermRe.FindStringSubmatch(term)
			if m == nil {
				continue
			}
			if refersToTable(m[1], m[2], table, alias, allowed) {
				add(&ordering, m[2])
			}
		}
	}

	cols = append(cols, equality...)
	if len(ranged) > 0 {
		cols = append(cols, ranged[0])
	}
	cols = append(cols, ordering...)
	if len(cols) > 4 {
		cols = cols[:4]
	}
	if len(cols) > 0 {
		unsargableCol = ""
	}
	return cols, unsargableCol
}

// wrappedColumn reports the name of one of table's columns if cond wraps it
// in a function call, e.g. "strftime('%Y-%m', order_date)" wraps order_date.
func wrappedColumn(cond, table string, allowed map[string]bool) string {
	if !strings.Contains(cond, "(") {
		return ""
	}
	for col := range allowed {
		re := regexp.MustCompile(`(?i)[A-Za-z_]\w*\s*\([^()]*\b` + regexp.QuoteMeta(col) + `\b[^()]*\)`)
		if re.MatchString(cond) {
			return col
		}
	}
	return ""
}

// splitTopLevel splits s on occurrences of sep (a keyword like "AND") that
// sit outside parentheses and single-quoted string literals, so a value
// like "status IN ('a AND b')" or a BETWEEN's own AND isn't split apart.
func splitTopLevel(s, sep string) []string {
	upperSep := strings.ToUpper(sep)
	var parts []string
	depth := 0
	inQuote := false
	start := 0
	upper := strings.ToUpper(s)
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == '\'':
			inQuote = !inQuote
		case inQuote:
			// skip
		case c == '(':
			depth++
		case c == ')':
			depth--
		case depth == 0 && matchesWordAt(upper, i, upperSep):
			parts = append(parts, s[start:i])
			i += len(sep)
			start = i
			continue
		}
		i++
	}
	parts = append(parts, s[start:])
	return parts
}

func matchesWordAt(upper string, i int, word string) bool {
	if i+len(word) > len(upper) || upper[i:i+len(word)] != word {
		return false
	}
	if i > 0 && isWordByte(upper[i-1]) {
		return false
	}
	end := i + len(word)
	if end < len(upper) && isWordByte(upper[end]) {
		return false
	}
	return true
}

func isWordByte(b byte) bool {
	return b == '_' || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}
