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

CREATE TABLE categories (
	id          INTEGER PRIMARY KEY,
	name        TEXT NOT NULL,
	description TEXT NOT NULL
);

CREATE TABLE suppliers (
	id      INTEGER PRIMARY KEY,
	name    TEXT NOT NULL,
	country TEXT NOT NULL,
	rating  REAL NOT NULL
);

CREATE TABLE product_suppliers (
	id          INTEGER PRIMARY KEY,
	product_id  INTEGER NOT NULL,
	supplier_id INTEGER NOT NULL,
	cost        REAL NOT NULL
);

CREATE TABLE warehouses (
	id      INTEGER PRIMARY KEY,
	name    TEXT NOT NULL,
	city    TEXT NOT NULL,
	country TEXT NOT NULL
);

CREATE TABLE inventory (
	id           INTEGER PRIMARY KEY,
	product_id   INTEGER NOT NULL,
	warehouse_id INTEGER NOT NULL,
	quantity     INTEGER NOT NULL
);

CREATE TABLE addresses (
	id          INTEGER PRIMARY KEY,
	customer_id INTEGER NOT NULL,
	line1       TEXT NOT NULL,
	city        TEXT NOT NULL,
	country     TEXT NOT NULL,
	is_default  INTEGER NOT NULL
);

CREATE TABLE payments (
	id       INTEGER PRIMARY KEY,
	order_id INTEGER NOT NULL,
	method   TEXT NOT NULL,
	amount   REAL NOT NULL,
	paid_at  TEXT NOT NULL,
	status   TEXT NOT NULL
);

CREATE TABLE employees (
	id       INTEGER PRIMARY KEY,
	name     TEXT NOT NULL,
	role     TEXT NOT NULL,
	hired_at TEXT NOT NULL
);

CREATE TABLE shipments (
	id           INTEGER PRIMARY KEY,
	order_id     INTEGER NOT NULL,
	warehouse_id INTEGER NOT NULL,
	employee_id  INTEGER NOT NULL,
	carrier      TEXT NOT NULL,
	shipped_at   TEXT NOT NULL,
	status       TEXT NOT NULL
);

CREATE TABLE reviews (
	id          INTEGER PRIMARY KEY,
	product_id  INTEGER NOT NULL,
	customer_id INTEGER NOT NULL,
	rating      INTEGER NOT NULL,
	title       TEXT NOT NULL,
	created_at  TEXT NOT NULL
);

CREATE TABLE discounts (
	id          INTEGER PRIMARY KEY,
	code        TEXT NOT NULL,
	percent_off REAL NOT NULL,
	active      INTEGER NOT NULL
);

CREATE TABLE order_discounts (
	id          INTEGER PRIMARY KEY,
	order_id    INTEGER NOT NULL,
	discount_id INTEGER NOT NULL
);

CREATE TABLE returns (
	id         INTEGER PRIMARY KEY,
	order_id   INTEGER NOT NULL,
	reason     TEXT NOT NULL,
	status     TEXT NOT NULL,
	created_at TEXT NOT NULL
);

CREATE TABLE return_items (
	id         INTEGER PRIMARY KEY,
	return_id  INTEGER NOT NULL,
	product_id INTEGER NOT NULL,
	quantity   INTEGER NOT NULL
);

CREATE TABLE wishlists (
	id          INTEGER PRIMARY KEY,
	customer_id INTEGER NOT NULL,
	name        TEXT NOT NULL,
	created_at  TEXT NOT NULL
);

CREATE TABLE wishlist_items (
	id          INTEGER PRIMARY KEY,
	wishlist_id INTEGER NOT NULL,
	product_id  INTEGER NOT NULL
);
`

// AllowedColumns is the whitelist guard.go uses to validate CREATE/DROP
// INDEX statements — a visitor (or the AI) may only index columns that
// actually exist on the four demo tables.
var AllowedColumns = map[string]map[string]bool{
	"customers":         {"id": true, "name": true, "email": true, "city": true, "country": true, "created_at": true},
	"products":          {"id": true, "name": true, "category": true, "price": true},
	"orders":            {"id": true, "customer_id": true, "status": true, "order_date": true, "total": true},
	"order_items":       {"id": true, "order_id": true, "product_id": true, "quantity": true, "unit_price": true},
	"categories":        {"id": true, "name": true, "description": true},
	"suppliers":         {"id": true, "name": true, "country": true, "rating": true},
	"product_suppliers": {"id": true, "product_id": true, "supplier_id": true, "cost": true},
	"warehouses":        {"id": true, "name": true, "city": true, "country": true},
	"inventory":         {"id": true, "product_id": true, "warehouse_id": true, "quantity": true},
	"addresses":         {"id": true, "customer_id": true, "line1": true, "city": true, "country": true, "is_default": true},
	"payments":          {"id": true, "order_id": true, "method": true, "amount": true, "paid_at": true, "status": true},
	"employees":         {"id": true, "name": true, "role": true, "hired_at": true},
	"shipments":         {"id": true, "order_id": true, "warehouse_id": true, "employee_id": true, "carrier": true, "shipped_at": true, "status": true},
	"reviews":           {"id": true, "product_id": true, "customer_id": true, "rating": true, "title": true, "created_at": true},
	"discounts":         {"id": true, "code": true, "percent_off": true, "active": true},
	"order_discounts":   {"id": true, "order_id": true, "discount_id": true},
	"returns":           {"id": true, "order_id": true, "reason": true, "status": true, "created_at": true},
	"return_items":      {"id": true, "return_id": true, "product_id": true, "quantity": true},
	"wishlists":         {"id": true, "customer_id": true, "name": true, "created_at": true},
	"wishlist_items":    {"id": true, "wishlist_id": true, "product_id": true},
}

// demoTables lists every table in FK-safe dependency order — tables with no
// references to other demo tables first, so Seed's per-table copy loop
// works whether or not foreign keys end up enforced.
var demoTables = []string{
	"categories", "suppliers", "warehouses", "employees", "discounts",
	"customers", "products",
	"product_suppliers", "inventory", "addresses",
	"orders", "order_items",
	"payments", "shipments", "reviews", "order_discounts",
	"returns", "return_items", "wishlists", "wishlist_items",
}

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
		{Name: "categories", Columns: []Column{
			{"id", "INTEGER"}, {"name", "TEXT"}, {"description", "TEXT"},
		}},
		{Name: "suppliers", Columns: []Column{
			{"id", "INTEGER"}, {"name", "TEXT"}, {"country", "TEXT"}, {"rating", "REAL"},
		}},
		{Name: "product_suppliers", Columns: []Column{
			{"id", "INTEGER"}, {"product_id", "INTEGER"}, {"supplier_id", "INTEGER"}, {"cost", "REAL"},
		}},
		{Name: "warehouses", Columns: []Column{
			{"id", "INTEGER"}, {"name", "TEXT"}, {"city", "TEXT"}, {"country", "TEXT"},
		}},
		{Name: "inventory", Columns: []Column{
			{"id", "INTEGER"}, {"product_id", "INTEGER"}, {"warehouse_id", "INTEGER"}, {"quantity", "INTEGER"},
		}},
		{Name: "addresses", Columns: []Column{
			{"id", "INTEGER"}, {"customer_id", "INTEGER"}, {"line1", "TEXT"},
			{"city", "TEXT"}, {"country", "TEXT"}, {"is_default", "INTEGER"},
		}},
		{Name: "payments", Columns: []Column{
			{"id", "INTEGER"}, {"order_id", "INTEGER"}, {"method", "TEXT"},
			{"amount", "REAL"}, {"paid_at", "TEXT"}, {"status", "TEXT"},
		}},
		{Name: "employees", Columns: []Column{
			{"id", "INTEGER"}, {"name", "TEXT"}, {"role", "TEXT"}, {"hired_at", "TEXT"},
		}},
		{Name: "shipments", Columns: []Column{
			{"id", "INTEGER"}, {"order_id", "INTEGER"}, {"warehouse_id", "INTEGER"}, {"employee_id", "INTEGER"},
			{"carrier", "TEXT"}, {"shipped_at", "TEXT"}, {"status", "TEXT"},
		}},
		{Name: "reviews", Columns: []Column{
			{"id", "INTEGER"}, {"product_id", "INTEGER"}, {"customer_id", "INTEGER"},
			{"rating", "INTEGER"}, {"title", "TEXT"}, {"created_at", "TEXT"},
		}},
		{Name: "discounts", Columns: []Column{
			{"id", "INTEGER"}, {"code", "TEXT"}, {"percent_off", "REAL"}, {"active", "INTEGER"},
		}},
		{Name: "order_discounts", Columns: []Column{
			{"id", "INTEGER"}, {"order_id", "INTEGER"}, {"discount_id", "INTEGER"},
		}},
		{Name: "returns", Columns: []Column{
			{"id", "INTEGER"}, {"order_id", "INTEGER"}, {"reason", "TEXT"},
			{"status", "TEXT"}, {"created_at", "TEXT"},
		}},
		{Name: "return_items", Columns: []Column{
			{"id", "INTEGER"}, {"return_id", "INTEGER"}, {"product_id", "INTEGER"}, {"quantity", "INTEGER"},
		}},
		{Name: "wishlists", Columns: []Column{
			{"id", "INTEGER"}, {"customer_id", "INTEGER"}, {"name", "TEXT"}, {"created_at", "TEXT"},
		}},
		{Name: "wishlist_items", Columns: []Column{
			{"id", "INTEGER"}, {"wishlist_id", "INTEGER"}, {"product_id", "INTEGER"},
		}},
	}
}
