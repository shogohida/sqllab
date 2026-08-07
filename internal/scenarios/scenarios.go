// Package scenarios holds the canned "slow query" stories the demo walks a
// visitor through: run the baseline query unindexed, see the scan and the
// timing, add the suggested index, rerun the same query, see it flip to a
// search. This is the interactive, reproducible version of the résumé's
// "40% improvement via load profiling and indexing" claim.
package scenarios

// Scenario pairs a realistic unindexed query with the exact index that
// fixes it. SuggestedIndexSQL is intentionally the literal statement a
// visitor's "Add suggested index" button submits — it must already satisfy
// internal/db's guard (known table, known columns).
type Scenario struct {
	ID                string `json:"id"`
	Title             string `json:"title"`
	Description       string `json:"description"`
	Query             string `json:"query"`
	SuggestedIndexSQL string `json:"suggested_index_sql"`
	AskAIPrompt       string `json:"ask_ai_prompt"` // example natural-language phrasing of Query, for the "Ask AI" panel
}

var All = []Scenario{
	{
		ID:    "customer-order-history",
		Title: "Customer order history",
		Description: "A customer support rep pulls up every order a customer has placed, " +
			"most recent first — one of the most common lookups in any e-commerce backend.",
		Query:             "SELECT * FROM orders WHERE customer_id = 42 ORDER BY order_date DESC",
		SuggestedIndexSQL: "CREATE INDEX idx_orders_customer_date ON orders(customer_id, order_date)",
		AskAIPrompt:       "Show me all orders for customer 42, most recent first",
	},
	{
		ID:    "order-line-items",
		Title: "Order line items",
		Description: "Loading an order's detail page means fetching every line item that " +
			"belongs to it.",
		Query:             "SELECT * FROM order_items WHERE order_id = 777",
		SuggestedIndexSQL: "CREATE INDEX idx_items_order ON order_items(order_id)",
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
		AskAIPrompt:       "What was completed revenue by city for Q3 2025?",
	},
	{
		ID:    "product-search",
		Title: "Product search by category and price",
		Description: "A storefront filters the catalog to one category under a price ceiling, " +
			"cheapest first — a typical faceted-search query.",
		Query:             "SELECT * FROM products WHERE category = 'Electronics' AND price < 100 ORDER BY price",
		SuggestedIndexSQL: "CREATE INDEX idx_products_category_price ON products(category, price)",
		AskAIPrompt:       "Find Electronics products under $100, cheapest first",
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
