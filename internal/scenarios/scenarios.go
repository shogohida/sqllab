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
// single "fix" button runs whichever of the two fields is set: index only,
// rewrite only, or both together. Both fields, like Query itself, must
// already satisfy internal/db's guard (known table, known columns).
type Scenario struct {
	ID                string `json:"id"`
	Title             string `json:"title"`
	Description       string `json:"description"`
	Query             string `json:"query"`
	SuggestedIndexSQL string `json:"suggested_index_sql,omitempty"`
	RewrittenQuery    string `json:"rewritten_query,omitempty"`
	FixExplanation    string `json:"fix_explanation"` // why the fix works, shown next to the fix button
	AskAIPrompt       string `json:"ask_ai_prompt"`   // example natural-language phrasing of Query, for the "Ask AI" panel
}

var All = []Scenario{
	{
		ID:    "customer-order-history",
		Title: "Customer order history",
		Description: "A customer support rep pulls up every order a customer has placed, " +
			"most recent first — one of the most common lookups in any e-commerce backend.",
		Query:             "SELECT * FROM orders WHERE customer_id = 42 ORDER BY order_date DESC",
		SuggestedIndexSQL: "CREATE INDEX idx_orders_customer_date ON orders(customer_id, order_date)",
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
		SuggestedIndexSQL: "CREATE INDEX idx_items_order ON order_items(order_id)",
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
		SuggestedIndexSQL: "CREATE INDEX idx_orders_status_date ON orders(status, order_date)",
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
		SuggestedIndexSQL: "CREATE INDEX idx_products_category_price ON products(category, price)",
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
		SuggestedIndexSQL: "CREATE INDEX IF NOT EXISTS idx_orders_order_date ON orders(order_date)",
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
		SuggestedIndexSQL: "CREATE INDEX IF NOT EXISTS idx_orders_order_date ON orders(order_date)",
		RewrittenQuery:    "SELECT * FROM orders WHERE order_date < '2025-06-01' ORDER BY order_date DESC LIMIT 20",
		FixExplanation: "The index makes the ordering itself free, but OFFSET still discards the " +
			"first 40,000 matched rows one at a time — there's no way around that cost with " +
			"LIMIT/OFFSET. Real pagination remembers the last row seen and seeks forward from " +
			"there (\"keyset pagination\") instead of counting through everything before it.",
		AskAIPrompt: "Skip the first 40,000 orders by date, most recent first, and show me the next 20",
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
