// Package db owns the demo dataset schema, per-session seeding, and the
// safety guardrails applied to every visitor-supplied (or AI-generated)
// SQL statement before it touches a session's SQLite connection.
package db

// schemaSQL creates the four demo tables with no indexes beyond SQLite's
// implicit rowid index on each INTEGER PRIMARY KEY. That absence is
// deliberate: the whole point of the demo is to let a visitor feel the
// difference between a full table scan and an index search on the exact
// same query, by adding the missing index themselves.
const schemaSQL = `
CREATE TABLE customers (
	id         INTEGER PRIMARY KEY,
	name       TEXT NOT NULL,
	email      TEXT NOT NULL,
	city       TEXT NOT NULL,
	country    TEXT NOT NULL,
	created_at TEXT NOT NULL
);

CREATE TABLE products (
	id       INTEGER PRIMARY KEY,
	name     TEXT NOT NULL,
	category TEXT NOT NULL,
	price    REAL NOT NULL
);

CREATE TABLE orders (
	id          INTEGER PRIMARY KEY,
	customer_id INTEGER NOT NULL,
	status      TEXT NOT NULL,
	order_date  TEXT NOT NULL,
	total       REAL NOT NULL
);

CREATE TABLE order_items (
	id         INTEGER PRIMARY KEY,
	order_id   INTEGER NOT NULL,
	product_id INTEGER NOT NULL,
	quantity   INTEGER NOT NULL,
	unit_price REAL NOT NULL
);
`

// AllowedColumns is the whitelist guard.go uses to validate CREATE/DROP
// INDEX statements — a visitor (or the AI) may only index columns that
// actually exist on the four demo tables.
var AllowedColumns = map[string]map[string]bool{
	"customers":   {"id": true, "name": true, "email": true, "city": true, "country": true, "created_at": true},
	"products":    {"id": true, "name": true, "category": true, "price": true},
	"orders":      {"id": true, "customer_id": true, "status": true, "order_date": true, "total": true},
	"order_items": {"id": true, "order_id": true, "product_id": true, "quantity": true, "unit_price": true},
}

var demoTables = []string{"customers", "products", "orders", "order_items"}

// Column and Table describe the demo schema for API consumers (the
// frontend's schema browser and the system prompt fed to the in-browser
// AI) without duplicating the column whitelist above.
type Column struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type Table struct {
	Name    string   `json:"name"`
	Columns []Column `json:"columns"`
}

// Describe returns the demo schema in table-definition order, matching
// schemaSQL.
func Describe() []Table {
	return []Table{
		{Name: "customers", Columns: []Column{
			{"id", "INTEGER"}, {"name", "TEXT"}, {"email", "TEXT"},
			{"city", "TEXT"}, {"country", "TEXT"}, {"created_at", "TEXT"},
		}},
		{Name: "products", Columns: []Column{
			{"id", "INTEGER"}, {"name", "TEXT"}, {"category", "TEXT"}, {"price", "REAL"},
		}},
		{Name: "orders", Columns: []Column{
			{"id", "INTEGER"}, {"customer_id", "INTEGER"}, {"status", "TEXT"},
			{"order_date", "TEXT"}, {"total", "REAL"},
		}},
		{Name: "order_items", Columns: []Column{
			{"id", "INTEGER"}, {"order_id", "INTEGER"}, {"product_id", "INTEGER"},
			{"quantity", "INTEGER"}, {"unit_price", "REAL"},
		}},
	}
}
