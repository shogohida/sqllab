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
// each session copy is tens of MB, so MaxSessions concurrent sessions
// comfortably fit; the free tier's throttled CPU (0.1 vCPU) is what
// actually makes an unindexed scan feel slow, not sheer row count.
const (
	numCustomers  = 3200
	numProducts   = 650
	numOrders     = 75000
	itemsPerOrder = 3 // average; actual count per order varies 1-5

	numSuppliers        = 35
	numWarehouses       = 10
	numEmployees        = 25
	numDiscounts        = 20
	numProductSuppliers = 800  // ~1-2 suppliers per product
	numInventory        = 2400 // ~3-4 warehouses stocking each product
	numAddresses        = 4000 // ~1.25 per customer
	numPayments         = 45000
	numShipments        = 40000
	numReviews          = 30000
	numOrderDiscounts   = 9000
	numReturns          = 3000
	numReturnItems      = 4000
	numWishlists        = 1200
	numWishlistItems    = 3000
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

	categoryDescriptions = map[string]string{
		"Electronics": "Gadgets, devices, and accessories",
		"Books":       "Fiction, non-fiction, and reference titles",
		"Home":        "Furniture, decor, and household goods",
		"Sports":      "Equipment and apparel for sports and fitness",
		"Toys":        "Toys and games for all ages",
		"Grocery":     "Packaged food and household staples",
		"Apparel":     "Clothing and accessories",
		"Beauty":      "Skincare, cosmetics, and personal care",
	}
	paymentMethods   = []string{"credit_card", "debit_card", "paypal", "bank_transfer", "gift_card"}
	paymentStatuses  = []string{"paid", "paid", "paid", "pending", "failed"} // weighted toward paid
	carriers         = []string{"UPS", "FedEx", "DHL", "USPS", "Japan Post"}
	shipmentStatuses = []string{"delivered", "delivered", "delivered", "in_transit", "returned"}
	employeeRoles    = []string{"warehouse", "support", "sales", "logistics"}
	reviewTitles     = []string{"Great product!", "Not what I expected", "Works as advertised", "Would buy again", "Disappointed", "Excellent quality", "Just okay", "Highly recommend"}
	returnReasons    = []string{"defective", "wrong item", "no longer needed", "damaged in transit", "changed mind"}
	returnStatuses   = []string{"approved", "pending", "rejected", "refunded"}
	addressStreets   = []string{"Main St", "Oak Ave", "Maple Dr", "Cedar Ln", "Park Rd", "1st Ave", "Sakura St", "River Rd"}
	wishlistNames    = []string{"Birthday wishlist", "Holiday gifts", "Home upgrade", "Someday", "Favorites"}
)

// pick returns a uniformly random element of options.
func pick[T any](rng *rand.Rand, options []T) T {
	return options[rng.Intn(len(options))]
}

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

	// FK-safe order: a table's referenced tables are always seeded first.
	seeders := []func(*sql.Tx, *rand.Rand) error{
		seedCategories, seedSuppliers, seedWarehouses, seedEmployees, seedDiscounts,
		seedCustomers, seedProducts,
		seedProductSuppliers, seedInventory, seedAddresses,
		seedOrdersAndItems,
		seedPayments, seedShipments, seedReviews, seedOrderDiscounts,
		seedReturnsAndItems, seedWishlistsAndItems,
	}
	for _, seed := range seeders {
		if err := seed(tx, rng); err != nil {
			tx.Rollback()
			return "", err
		}
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

func seedCategories(tx *sql.Tx, rng *rand.Rand) error {
	stmt, err := tx.Prepare(`INSERT INTO categories (id, name, description) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for i, name := range categories {
		if _, err := stmt.Exec(i+1, name, categoryDescriptions[name]); err != nil {
			return err
		}
	}
	return nil
}

func seedSuppliers(tx *sql.Tx, rng *rand.Rand) error {
	stmt, err := tx.Prepare(`INSERT INTO suppliers (id, name, country, rating) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for i := 1; i <= numSuppliers; i++ {
		name := fmt.Sprintf("%s Supply Co.", pick(rng, lastNames))
		country := pick(rng, countries)
		rating := roundCents(2.5 + rng.Float64()*2.5) // 2.5-5.0
		if _, err := stmt.Exec(i, name, country, rating); err != nil {
			return err
		}
	}
	return nil
}

func seedWarehouses(tx *sql.Tx, rng *rand.Rand) error {
	stmt, err := tx.Prepare(`INSERT INTO warehouses (id, name, city, country) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for i := 1; i <= numWarehouses; i++ {
		city := pick(rng, cities)
		name := fmt.Sprintf("%s Distribution Center", city)
		country := "Japan"
		if _, err := stmt.Exec(i, name, city, country); err != nil {
			return err
		}
	}
	return nil
}

func seedEmployees(tx *sql.Tx, rng *rand.Rand) error {
	stmt, err := tx.Prepare(`INSERT INTO employees (id, name, role, hired_at) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for i := 1; i <= numEmployees; i++ {
		name := fmt.Sprintf("%s %s", pick(rng, firstNames), pick(rng, lastNames))
		role := pick(rng, employeeRoles)
		hiredAt := randomDate(rng, seedEpoch, seedHorizon)
		if _, err := stmt.Exec(i, name, role, hiredAt); err != nil {
			return err
		}
	}
	return nil
}

func seedDiscounts(tx *sql.Tx, rng *rand.Rand) error {
	stmt, err := tx.Prepare(`INSERT INTO discounts (id, code, percent_off, active) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for i := 1; i <= numDiscounts; i++ {
		code := fmt.Sprintf("SAVE%d", 5+rng.Intn(40))
		percentOff := float64(5 + rng.Intn(40))
		active := 1
		if rng.Intn(4) == 0 {
			active = 0
		}
		if _, err := stmt.Exec(i, code, percentOff, active); err != nil {
			return err
		}
	}
	return nil
}

func seedProductSuppliers(tx *sql.Tx, rng *rand.Rand) error {
	stmt, err := tx.Prepare(`INSERT INTO product_suppliers (id, product_id, supplier_id, cost) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	id := 1
	for productID := 1; productID <= numProducts; productID++ {
		n := 1 + rng.Intn(2) // 1-2 suppliers per product
		for j := 0; j < n && id <= numProductSuppliers; j++ {
			supplierID := rng.Intn(numSuppliers) + 1
			cost := roundCents(2 + rng.Float64()*300)
			if _, err := stmt.Exec(id, productID, supplierID, cost); err != nil {
				return err
			}
			id++
		}
	}
	return nil
}

func seedInventory(tx *sql.Tx, rng *rand.Rand) error {
	stmt, err := tx.Prepare(`INSERT INTO inventory (id, product_id, warehouse_id, quantity) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for i := 1; i <= numInventory; i++ {
		productID := rng.Intn(numProducts) + 1
		warehouseID := rng.Intn(numWarehouses) + 1
		quantity := rng.Intn(500)
		if _, err := stmt.Exec(i, productID, warehouseID, quantity); err != nil {
			return err
		}
	}
	return nil
}

func seedAddresses(tx *sql.Tx, rng *rand.Rand) error {
	stmt, err := tx.Prepare(`INSERT INTO addresses (id, customer_id, line1, city, country, is_default) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for i := 1; i <= numAddresses; i++ {
		customerID := rng.Intn(numCustomers) + 1
		line1 := fmt.Sprintf("%d %s", 1+rng.Intn(9999), pick(rng, addressStreets))
		city := pick(rng, cities)
		country := pick(rng, countries)
		isDefault := 0
		if rng.Intn(3) == 0 {
			isDefault = 1
		}
		if _, err := stmt.Exec(i, customerID, line1, city, country, isDefault); err != nil {
			return err
		}
	}
	return nil
}

func seedPayments(tx *sql.Tx, rng *rand.Rand) error {
	stmt, err := tx.Prepare(`INSERT INTO payments (id, order_id, method, amount, paid_at, status) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for i := 1; i <= numPayments; i++ {
		orderID := rng.Intn(numOrders) + 1
		method := pick(rng, paymentMethods)
		amount := roundCents(5 + rng.Float64()*995)
		paidAt := randomDate(rng, seedEpoch, seedHorizon)
		status := pick(rng, paymentStatuses)
		if _, err := stmt.Exec(i, orderID, method, amount, paidAt, status); err != nil {
			return err
		}
	}
	return nil
}

func seedShipments(tx *sql.Tx, rng *rand.Rand) error {
	stmt, err := tx.Prepare(`INSERT INTO shipments (id, order_id, warehouse_id, employee_id, carrier, shipped_at, status) VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for i := 1; i <= numShipments; i++ {
		orderID := rng.Intn(numOrders) + 1
		warehouseID := rng.Intn(numWarehouses) + 1
		employeeID := rng.Intn(numEmployees) + 1
		carrier := pick(rng, carriers)
		shippedAt := randomDate(rng, seedEpoch, seedHorizon)
		status := pick(rng, shipmentStatuses)
		if _, err := stmt.Exec(i, orderID, warehouseID, employeeID, carrier, shippedAt, status); err != nil {
			return err
		}
	}
	return nil
}

func seedReviews(tx *sql.Tx, rng *rand.Rand) error {
	stmt, err := tx.Prepare(`INSERT INTO reviews (id, product_id, customer_id, rating, title, created_at) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for i := 1; i <= numReviews; i++ {
		productID := rng.Intn(numProducts) + 1
		customerID := rng.Intn(numCustomers) + 1
		rating := 1 + rng.Intn(5)
		title := pick(rng, reviewTitles)
		createdAt := randomDate(rng, seedEpoch, seedHorizon)
		if _, err := stmt.Exec(i, productID, customerID, rating, title, createdAt); err != nil {
			return err
		}
	}
	return nil
}

func seedOrderDiscounts(tx *sql.Tx, rng *rand.Rand) error {
	stmt, err := tx.Prepare(`INSERT INTO order_discounts (id, order_id, discount_id) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for i := 1; i <= numOrderDiscounts; i++ {
		orderID := rng.Intn(numOrders) + 1
		discountID := rng.Intn(numDiscounts) + 1
		if _, err := stmt.Exec(i, orderID, discountID); err != nil {
			return err
		}
	}
	return nil
}

func seedReturnsAndItems(tx *sql.Tx, rng *rand.Rand) error {
	returnStmt, err := tx.Prepare(`INSERT INTO returns (id, order_id, reason, status, created_at) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer returnStmt.Close()

	itemStmt, err := tx.Prepare(`INSERT INTO return_items (id, return_id, product_id, quantity) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer itemStmt.Close()

	itemID := 1
	for returnID := 1; returnID <= numReturns; returnID++ {
		orderID := rng.Intn(numOrders) + 1
		reason := pick(rng, returnReasons)
		status := pick(rng, returnStatuses)
		createdAt := randomDate(rng, seedEpoch, seedHorizon)
		if _, err := returnStmt.Exec(returnID, orderID, reason, status, createdAt); err != nil {
			return err
		}

		if itemID <= numReturnItems {
			productID := rng.Intn(numProducts) + 1
			quantity := 1 + rng.Intn(3)
			if _, err := itemStmt.Exec(itemID, returnID, productID, quantity); err != nil {
				return err
			}
			itemID++
		}
	}
	// Remaining return_items (returns can have more than one line item).
	for ; itemID <= numReturnItems; itemID++ {
		returnID := rng.Intn(numReturns) + 1
		productID := rng.Intn(numProducts) + 1
		quantity := 1 + rng.Intn(3)
		if _, err := itemStmt.Exec(itemID, returnID, productID, quantity); err != nil {
			return err
		}
	}
	return nil
}

func seedWishlistsAndItems(tx *sql.Tx, rng *rand.Rand) error {
	wishlistStmt, err := tx.Prepare(`INSERT INTO wishlists (id, customer_id, name, created_at) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer wishlistStmt.Close()

	itemStmt, err := tx.Prepare(`INSERT INTO wishlist_items (id, wishlist_id, product_id) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer itemStmt.Close()

	itemID := 1
	for wishlistID := 1; wishlistID <= numWishlists; wishlistID++ {
		customerID := rng.Intn(numCustomers) + 1
		name := pick(rng, wishlistNames)
		createdAt := randomDate(rng, seedEpoch, seedHorizon)
		if _, err := wishlistStmt.Exec(wishlistID, customerID, name, createdAt); err != nil {
			return err
		}

		n := 1 + rng.Intn(4) // 1-4 items per wishlist
		for j := 0; j < n && itemID <= numWishlistItems; j++ {
			productID := rng.Intn(numProducts) + 1
			if _, err := itemStmt.Exec(itemID, wishlistID, productID); err != nil {
				return err
			}
			itemID++
		}
	}
	// Remaining wishlist_items, if the per-wishlist loop didn't use them all.
	for ; itemID <= numWishlistItems; itemID++ {
		wishlistID := rng.Intn(numWishlists) + 1
		productID := rng.Intn(numProducts) + 1
		if _, err := itemStmt.Exec(itemID, wishlistID, productID); err != nil {
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
// in-process copy on the order of a second at this dataset's size, versus
// re-running the full row-by-row generator per visitor.
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
