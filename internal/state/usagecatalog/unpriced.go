package usagecatalog

import (
	"context"
	"sort"
)

// UnpricedInRange counts the range's unpriced calls and names the distinct
// reasons their rows recorded, sorted. It reads the stored predicate, so it
// answers what the exact JSONL read answers.
func (c *Catalog) UnpricedInRange(ctx context.Context, fromDay, toDay, source string) (int, []string, error) {
	where := `day>=? AND day<=? AND cost_incomplete=1`
	args := []any{fromDay, toDay}
	if source != "" && source != "all" {
		where += ` AND source=?`
		args = append(args, source)
	}
	rows, err := c.db.QueryContext(ctx,
		`SELECT incomplete_reason,COUNT(*) FROM usage_records WHERE `+where+` GROUP BY incomplete_reason`, args...)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	var calls int
	var reasons []string
	for rows.Next() {
		var reason string
		var n int
		if err := rows.Scan(&reason, &n); err != nil {
			return 0, nil, err
		}
		calls += n
		if reason != "" {
			reasons = append(reasons, reason)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, nil, err
	}
	// Sorted, deduplicated by the GROUP BY: a repeated query returns the same
	// answer, and an unnamed reason stays out of the vocabulary rather than
	// entering it as an empty string.
	sort.Strings(reasons)
	return calls, reasons, nil
}
