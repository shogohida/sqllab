// Package scenarios holds the canned "slow query" stories the demo walks a
// visitor through: run the baseline query unindexed, see the scan and the
// timing, add the suggested index, rerun the same query, see it flip to a
// search. This is the interactive, reproducible version of the résumé's
// "40% improvement via load profiling and indexing" claim.
package scenarios

// Scenario pairs a realistic slow query with the fix that makes it fast.
// Most scenarios are pure indexing (SuggestedIndexSQL only), but some slow
// queries can't be fixed by an index alone — RewrittenQuery holds an
// equivalent restatement of Query that either avoids the problem outright
// or (combined with SuggestedIndexSQL) becomes sargable. The frontend's
// single "fix" button runs whichever of the two fields is set: index(es)
// only, rewrite only, or both together. SuggestedIndexSQL is a slice
// because a couple of scenarios (see needs-two-indexes below) genuinely
// need two separate indexes on two separate tables before either one
// changes the plan — see internal/db's guard for what each entry, like
// Query itself, must already satisfy (known table, known columns).
type Scenario struct {
	ID                string   `json:"id"`
	Title             string   `json:"title"`
	Description       string   `json:"description"`
	Query             string   `json:"query"`
	SuggestedIndexSQL []string `json:"suggested_index_sql,omitempty"`
	RewrittenQuery    string   `json:"rewritten_query,omitempty"`
	FixExplanation    string   `json:"fix_explanation"` // why the fix works, shown next to the fix button
	AskAIPrompt       string   `json:"ask_ai_prompt"`   // example natural-language phrasing of Query, for the "Ask AI" panel
}

var All = []Scenario{
	{
		ID:    "customer-order-history",
		Title: "Customer order history",
		Description: "A customer support rep pulls up every order a customer has placed, " +
			"most recent first — one of the most common lookups in any e-commerce backend.",
		Query:             "SELECT * FROM orders WHERE customer_id = 42 ORDER BY order_date DESC",
		SuggestedIndexSQL: []string{"CREATE INDEX idx_orders_customer_date ON orders(customer_id, order_date)"},
		FixExplanation: "An index on (customer_id, order_date) lets SQLite jump straight to this " +
			"customer's rows, already in date order — no scan, no separate sort.",
		AskAIPrompt: "Show me all orders for customer 42, most recent first",
	},
	{
		ID:    "order-line-items",
		Title: "Order line items",
		Description: "Loading an order's detail page means fetching every line item that " +
			"belongs to it.",
		Query:             "SELECT * FROM order_items WHERE order_id = 777",
		SuggestedIndexSQL: []string{"CREATE INDEX idx_items_order ON order_items(order_id)"},
		FixExplanation:    "An index on order_id turns a full table scan into a direct lookup for this order's rows.",
		AskAIPrompt:       "List every line item on order 777",
	},
	{
		ID:    "regional-revenue",
		Title: "Regional revenue in a date range",
		Description: "Finance asks for completed revenue by city over a specific quarter — a " +
			"join across all three other tables, filtered and grouped.",
		Query: "SELECT c.city, SUM(oi.quantity * oi.unit_price) AS revenue " +
			"FROM orders o " +
			"JOIN order_items oi ON oi.order_id = o.id " +
			"JOIN customers c ON c.id = o.customer_id " +
			"WHERE o.status = 'completed' AND o.order_date BETWEEN '2025-07-01' AND '2025-09-30' " +
			"GROUP BY c.city " +
			"ORDER BY revenue DESC",
		SuggestedIndexSQL: []string{"CREATE INDEX idx_orders_status_date ON orders(status, order_date)"},
		FixExplanation: "An index on (status, order_date) narrows the orders scan to just the " +
			"completed rows in this quarter before the joins even start.",
		AskAIPrompt: "What was completed revenue by city for Q3 2025?",
	},
	{
		ID:    "product-search",
		Title: "Product search by category and price",
		Description: "A storefront filters the catalog to one category under a price ceiling, " +
			"cheapest first — a typical faceted-search query.",
		Query:             "SELECT * FROM products WHERE category = 'Electronics' AND price < 100 ORDER BY price",
		SuggestedIndexSQL: []string{"CREATE INDEX idx_products_category_price ON products(category, price)"},
		FixExplanation: "An index on (category, price) lets SQLite jump straight to Electronics " +
			"under $100, already in price order.",
		AskAIPrompt: "Find Electronics products under $100, cheapest first",
	},
	{
		ID:    "unsargable-date-filter",
		Title: "Filtering by month with a wrapped date column",
		Description: "A reporting dashboard asks for every order placed in a given month. " +
			"Wrapping order_date in strftime() to pull out the year-month makes the predicate " +
			"something SQLite can't use an index for — even after adding one.",
		Query:             "SELECT * FROM orders WHERE strftime('%Y-%m', order_date) = '2025-08'",
		SuggestedIndexSQL: []string{"CREATE INDEX IF NOT EXISTS idx_orders_order_date ON orders(order_date)"},
		RewrittenQuery:    "SELECT * FROM orders WHERE order_date >= '2025-08-01' AND order_date < '2025-09-01'",
		FixExplanation: "An index on order_date alone doesn't help — SQLite can't use it through " +
			"a function call on the column, so it still has to scan every row. Rewriting the " +
			"filter as a plain range comparison (same result, no function call) is what lets the " +
			"index actually get used.",
		AskAIPrompt: "Show me every order placed in August 2025",
	},
	{
		ID:    "deep-pagination",
		Title: "Deep pagination through the order list",
		Description: "An order list jumps straight to page 2001 by skipping 40,000 rows with " +
			"OFFSET. Even with an index on order_date, SQLite still has to walk past every " +
			"skipped row before it can return the next page.",
		Query:             "SELECT * FROM orders ORDER BY order_date DESC LIMIT 20 OFFSET 40000",
		SuggestedIndexSQL: []string{"CREATE INDEX IF NOT EXISTS idx_orders_order_date ON orders(order_date)"},
		RewrittenQuery:    "SELECT * FROM orders WHERE order_date < '2025-06-01' ORDER BY order_date DESC LIMIT 20",
		FixExplanation: "The index makes the ordering itself free, but OFFSET still discards the " +
			"first 40,000 matched rows one at a time — there's no way around that cost with " +
			"LIMIT/OFFSET. Real pagination remembers the last row seen and seeks forward from " +
			"there (\"keyset pagination\") instead of counting through everything before it.",
		AskAIPrompt: "Skip the first 40,000 orders by date, most recent first, and show me the next 20",
	},
	{
		ID:    "product-reviews",
		Title: "Product reviews, most recent first",
		Description: "A product page loads every review for one item, newest first, alongside " +
			"the product's own name — a fact table joined out to a dimension table for a " +
			"human-readable label.",
		Query: "SELECT r.rating, r.title, r.created_at, p.name AS product_name " +
			"FROM reviews r " +
			"JOIN products p ON p.id = r.product_id " +
			"WHERE r.product_id = 42 " +
			"ORDER BY r.created_at DESC",
		SuggestedIndexSQL: []string{"CREATE INDEX idx_reviews_product_created ON reviews(product_id, created_at)"},
		FixExplanation: "An index on (product_id, created_at) lets SQLite jump straight to this " +
			"product's reviews, already newest-first — the join to products is a single " +
			"primary-key lookup either way, so it was never the bottleneck.",
		AskAIPrompt: "Show me all reviews for product 42, most recent first, with the product name",
	},
	{
		ID:    "failed-payments-followup",
		Title: "Failed payments needing follow-up",
		Description: "Support pulls every failed payment with the customer's contact info, " +
			"most recent first — a fact table (payments) joined out through orders to the " +
			"customer who needs a call.",
		Query: "SELECT c.name, c.email, o.id AS order_id, p.amount, p.paid_at " +
			"FROM payments p " +
			"JOIN orders o ON o.id = p.order_id " +
			"JOIN customers c ON c.id = o.customer_id " +
			"WHERE p.status = 'failed' " +
			"ORDER BY p.paid_at DESC",
		SuggestedIndexSQL: []string{"CREATE INDEX idx_payments_status_paid_at ON payments(status, paid_at)"},
		FixExplanation: "An index on (status, paid_at) narrows the payments scan to just the " +
			"failed ones, already in date order — both joins that follow are cheap " +
			"primary-key lookups (payments.order_id → orders.id, orders.customer_id → " +
			"customers.id), so indexing the fact table's filter column is the whole fix.",
		AskAIPrompt: "Show me every failed payment with the customer's name and email, most recent first",
	},
	{
		ID:    "warehouse-in-transit-shipments",
		Title: "In-transit shipments from one warehouse",
		Description: "Warehouse ops pulls every shipment currently in transit from one " +
			"warehouse, along with who's handling it, most recent first.",
		Query: "SELECT e.name AS handled_by, s.carrier, s.shipped_at " +
			"FROM shipments s " +
			"JOIN employees e ON e.id = s.employee_id " +
			"WHERE s.warehouse_id = 3 AND s.status = 'in_transit' " +
			"ORDER BY s.shipped_at DESC",
		SuggestedIndexSQL: []string{"CREATE INDEX idx_shipments_warehouse_status_shipped ON shipments(warehouse_id, status, shipped_at)"},
		FixExplanation: "An index on (warehouse_id, status, shipped_at) narrows the shipments " +
			"scan to just this warehouse's in-transit rows, already newest-first — the join " +
			"to employees for the handler's name is a cheap primary-key lookup.",
		AskAIPrompt: "Show me every in-transit shipment from warehouse 3, most recent first, with who's handling it",
	},
	{
		ID:    "orders-needing-review",
		Title: "Completed orders with a pending return and a failed payment",
		Description: "A risk-review queue flags completed orders that have both a return " +
			"still pending and a payment that failed — two independent red flags on two " +
			"different tables, checked against the same order.",
		Query: "SELECT o.id, o.customer_id, o.order_date, o.total " +
			"FROM orders o " +
			"WHERE o.status = 'completed' " +
			"AND EXISTS (SELECT 1 FROM returns r WHERE r.order_id = o.id AND r.status = 'pending') " +
			"AND EXISTS (SELECT 1 FROM payments p WHERE p.order_id = o.id AND p.status = 'failed') " +
			"ORDER BY o.order_date DESC",
		SuggestedIndexSQL: []string{
			"CREATE INDEX idx_returns_order_status ON returns(order_id, status)",
			"CREATE INDEX idx_payments_order_status ON payments(order_id, status)",
		},
		FixExplanation: "This one genuinely needs both indexes — they fix two independent " +
			"subqueries, not one shared bottleneck. Each EXISTS re-scans its own table once " +
			"per candidate order: without an index on returns(order_id, status), the pending-" +
			"return check alone is a full returns scan per order; without one on " +
			"payments(order_id, status), the failed-payment check alone is a full payments " +
			"scan per order. Adding only one still leaves the other subquery scanning — both " +
			"have to be in place before the query stops crawling every order.",
		AskAIPrompt: "Show me completed orders that have a pending return and a failed payment, most recent first",
	},
}

func ByID(id string) (Scenario, bool) {
	for _, s := range All {
		if s.ID == id {
			return s, true
		}
	}
	return Scenario{}, false
}
