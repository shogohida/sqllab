package db

import "testing"

func TestValidateStatement_Allowed(t *testing.T) {
	cases := []string{
		`SELECT * FROM orders WHERE customer_id = 1`,
		`select * from orders`,
		`EXPLAIN QUERY PLAN SELECT * FROM orders`,
		`CREATE INDEX idx_orders_customer_date ON orders(customer_id, order_date)`,
		`CREATE INDEX idx_products_cat ON products (category)`,
		`DROP INDEX idx_orders_customer_date`,
		`DROP INDEX IF EXISTS idx_orders_customer_date`,
		`SELECT * FROM orders;`, // single trailing semicolon is fine
	}
	for _, stmt := range cases {
		if _, _, err := ValidateStatement(stmt); err != nil {
			t.Errorf("expected %q to be allowed, got error: %v", stmt, err)
		}
	}
}

func TestValidateStatement_Rejected(t *testing.T) {
	cases := map[string]string{
		"stacked statements":      `SELECT * FROM orders; DROP TABLE orders`,
		"delete":                  `DELETE FROM orders WHERE id = 1`,
		"update":                  `UPDATE orders SET status = 'x'`,
		"drop table":              `DROP TABLE orders`,
		"attach":                  `SELECT * FROM orders; ATTACH DATABASE '/etc/passwd' AS x`,
		"pragma":                  `PRAGMA table_info(orders)`,
		"raw explain":             `EXPLAIN SELECT * FROM orders`,
		"unknown table on index":  `CREATE INDEX idx_x ON secrets(password)`,
		"unknown column on index": `CREATE INDEX idx_x ON orders(does_not_exist)`,
		"empty":                   ``,
		"attach without select":   `ATTACH DATABASE 'x' AS y`,
	}
	for name, stmt := range cases {
		if _, _, err := ValidateStatement(stmt); err == nil {
			t.Errorf("%s: expected %q to be rejected, got no error", name, stmt)
		}
	}
}

func TestValidateStatement_CreateIndexColumnNormalization(t *testing.T) {
	_, clean, err := ValidateStatement(`CREATE INDEX idx ON orders(status DESC, order_date ASC)`)
	if err != nil {
		t.Fatalf("expected DESC/ASC modifiers to be accepted, got: %v", err)
	}
	if clean == "" {
		t.Fatal("expected a cleaned statement back")
	}
}
