package db

import (
	"database/sql"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Dataset sizes are deliberately modest: Render's free web-service tier is
// 512MB RAM shared across the Go runtime and every concurrent session's
// in-memory copy of this data (see internal/session). At these row counts
// each session copy is a few MB, so ~20 concurrent sessions comfortably
// fit; the free tier's throttled CPU (0.1 vCPU) is what actually makes an
// unindexed scan feel slow, not sheer row count.
const (
	numCustomers  = 2000
	numProducts   = 500
	numOrders     = 50000
	itemsPerOrder = 3 // average; actual count per order varies 1-5
)

var (
	cities      = []string{"Tokyo", "Osaka", "Yokohama", "Nagoya", "Sapporo", "Fukuoka", "Kobe", "Kyoto", "Sendai", "Hiroshima"}
	countries   = []string{"Japan", "United States", "Germany", "Brazil", "Spain"}
	categories  = []string{"Electronics", "Books", "Home", "Sports", "Toys", "Grocery", "Apparel", "Beauty"}
	statuses    = []string{"completed", "completed", "completed", "pending", "cancelled", "refunded"} // weighted toward completed
	firstNames  = []string{"Yuki", "Sora", "Ren", "Aoi", "Haruto", "Mio", "Kaito", "Hina", "Sota", "Yui"}
	lastNames   = []string{"Sato", "Suzuki", "Takahashi", "Tanaka", "Watanabe", "Ito", "Yamamoto", "Nakamura", "Kobayashi", "Kato"}
	seedEpoch   = time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	seedHorizon = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
)

// BuildTemplate generates the demo dataset once, at process startup, into a
// fresh on-disk SQLite file under dir. The file is used as an ATTACHed
// source for fast per-session copies (see Seed) rather than embedded into
// the binary — it's cheap to regenerate deterministically (fixed RNG seed)
// and keeps the binary itself small.
func BuildTemplate(dir string) (path string, err error) {
	path = filepath.Join(dir, "sqllab-template.db")
	_ = os.Remove(path) // stale file from a previous run, if any

	templateDB, err := sql.Open("sqlite", path)
	if err != nil {
		return "", fmt.Errorf("open template db: %w", err)
	}
	defer templateDB.Close()
	templateDB.SetMaxOpenConns(1)

	if _, err := templateDB.Exec(schemaSQL); err != nil {
		return "", fmt.Errorf("apply schema: %w", err)
	}

	rng := rand.New(rand.NewSource(42)) // fixed seed: reproducible dataset across restarts

	tx, err := templateDB.Begin()
	if err != nil {
		return "", err
	}
	if err := seedCustomers(tx, rng); err != nil {
		tx.Rollback()
		return "", err
	}
	if err := seedProducts(tx, rng); err != nil {
		tx.Rollback()
		return "", err
	}
	if err := seedOrdersAndItems(tx, rng); err != nil {
		tx.Rollback()
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}

	return path, nil
}

func seedCustomers(tx *sql.Tx, rng *rand.Rand) error {
	stmt, err := tx.Prepare(`INSERT INTO customers (id, name, email, city, country, created_at) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for i := 1; i <= numCustomers; i++ {
		first := firstNames[rng.Intn(len(firstNames))]
		last := lastNames[rng.Intn(len(lastNames))]
		name := fmt.Sprintf("%s %s", first, last)
		email := fmt.Sprintf("%s.%s%d@example.com", strings.ToLower(first), strings.ToLower(last), i)
		city := cities[rng.Intn(len(cities))]
		country := countries[rng.Intn(len(countries))]
		createdAt := randomDate(rng, seedEpoch, seedHorizon)
		if _, err := stmt.Exec(i, name, email, city, country, createdAt); err != nil {
			return err
		}
	}
	return nil
}

func seedProducts(tx *sql.Tx, rng *rand.Rand) error {
	stmt, err := tx.Prepare(`INSERT INTO products (id, name, category, price) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for i := 1; i <= numProducts; i++ {
		category := categories[rng.Intn(len(categories))]
		name := fmt.Sprintf("%s Item %d", category, i)
		price := roundCents(5 + rng.Float64()*495)
		if _, err := stmt.Exec(i, name, category, price); err != nil {
			return err
		}
	}
	return nil
}

func seedOrdersAndItems(tx *sql.Tx, rng *rand.Rand) error {
	orderStmt, err := tx.Prepare(`INSERT INTO orders (id, customer_id, status, order_date, total) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer orderStmt.Close()

	itemStmt, err := tx.Prepare(`INSERT INTO order_items (id, order_id, product_id, quantity, unit_price) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer itemStmt.Close()

	itemID := 1
	for orderID := 1; orderID <= numOrders; orderID++ {
		customerID := rng.Intn(numCustomers) + 1
		status := statuses[rng.Intn(len(statuses))]
		orderDate := randomDate(rng, seedEpoch, seedHorizon)

		n := itemsPerOrder - 1 + rng.Intn(3) // 1-5 items per order, averaging ~3
		if n < 1 {
			n = 1
		}

		var total float64
		for j := 0; j < n; j++ {
			productID := rng.Intn(numProducts) + 1
			quantity := rng.Intn(4) + 1
			unitPrice := roundCents(5 + rng.Float64()*495)
			total += unitPrice * float64(quantity)

			if _, err := itemStmt.Exec(itemID, orderID, productID, quantity, unitPrice); err != nil {
				return err
			}
			itemID++
		}

		if _, err := orderStmt.Exec(orderID, customerID, status, orderDate, roundCents(total)); err != nil {
			return err
		}
	}
	return nil
}

func randomDate(rng *rand.Rand, from, to time.Time) string {
	delta := to.Sub(from)
	offset := time.Duration(rng.Int63n(int64(delta)))
	return from.Add(offset).Format("2006-01-02")
}

func roundCents(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

// Seed populates an empty session database (opened on ":memory:") by
// ATTACHing the shared template file and bulk-copying each table — an
// in-process copy that takes well under 100ms, versus re-running the full
// row-by-row generator per visitor.
func Seed(db *sql.DB, templatePath string) error {
	if _, err := db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}

	attachSQL := fmt.Sprintf("ATTACH DATABASE %s AS tmpl", sqlStringLiteral(templatePath))
	if _, err := db.Exec(attachSQL); err != nil {
		return fmt.Errorf("attach template: %w", err)
	}
	defer db.Exec("DETACH DATABASE tmpl")

	for _, table := range demoTables {
		copySQL := fmt.Sprintf("INSERT INTO %s SELECT * FROM tmpl.%s", table, table)
		if _, err := db.Exec(copySQL); err != nil {
			return fmt.Errorf("copy %s: %w", table, err)
		}
	}
	return nil
}

// sqlStringLiteral single-quotes s for use in a SQL statement, doubling any
// embedded single quotes per standard SQL escaping. Used instead of a
// placeholder because SQLite's ATTACH DATABASE does not accept bound
// parameters for the file path.
func sqlStringLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
