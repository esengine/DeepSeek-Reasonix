package usagecatalog

import (
	"context"
	"sort"
	"strings"
)

// Provenance is the range's call accounting: the denominator its money total
// is over, and the facts that say how each call's price was reached. One read
// answers all of them, so a count never describes a different row set from the
// reason beside it.
type Provenance struct {
	// CountedCalls is the denominator: one per non-turn row that carried
	// tokens, a price, or a quote that said it could not price the call.
	CountedCalls int
	// UnpricedCalls is the subset whose money is unknown; the money total that
	// folds them in is a floor.
	UnpricedCalls int
	// EstimatedCalls is the priced subset quoted from an estimated/fallback
	// rate card rather than a provider-reported amount.
	EstimatedCalls int
	// UnnamedCardCalls is the subset that recorded a price but no
	// pricing_fingerprint: the amount exists, the card behind it is unnamed.
	UnnamedCardCalls int
	// UnclassifiedCardCalls is the counted subset that recorded no price fact
	// at all, so whether its card was named was never recorded. It is unknown,
	// not zero.
	UnclassifiedCardCalls int
	// Reasons names the distinct reasons the unpriced rows recorded, sorted.
	// A reason is only listed when an unpriced row carries it.
	Reasons []string
}

// ProvenanceInRange reads the stored row flags. Every predicate is a flag the
// quote path already recorded, so this answers what the exact JSONL read
// answers; the reasons come from the same pass as the counts, not a second
// scan. The projection keeps sums, so the row count is read here rather than
// derived from a rollup.
func (c *Catalog) ProvenanceInRange(ctx context.Context, fromDay, toDay, source string) (Provenance, error) {
	where := `day>=? AND day<=? AND turns=0`
	args := []any{fromDay, toDay}
	if source != "" && source != "all" {
		where += ` AND source=?`
		args = append(args, source)
	}
	rows, err := c.db.QueryContext(ctx, `SELECT incomplete_reason,
        SUM(CASE WHEN total>0 OR cost_currency<>'' OR cost_incomplete=1 THEN 1 ELSE 0 END),
        SUM(cost_incomplete),
        SUM(CASE WHEN cost_incomplete=0 AND cost_currency<>'' AND cost_estimated=1 THEN 1 ELSE 0 END),
        SUM(CASE WHEN cost_currency<>'' AND pricing_fingerprint='' THEN 1 ELSE 0 END),
        SUM(CASE WHEN cost_currency='' AND (total>0 OR cost_incomplete=1) THEN 1 ELSE 0 END)
        FROM usage_records WHERE `+where+` GROUP BY incomplete_reason`, args...)
	if err != nil {
		return Provenance{}, err
	}
	defer rows.Close()
	var out Provenance
	seen := map[string]bool{}
	for rows.Next() {
		var reason string
		var counted, unpriced, estimated, unnamed, unclassified int
		if err := rows.Scan(&reason, &counted, &unpriced, &estimated, &unnamed, &unclassified); err != nil {
			return Provenance{}, err
		}
		out.CountedCalls += counted
		out.UnpricedCalls += unpriced
		out.EstimatedCalls += estimated
		out.UnnamedCardCalls += unnamed
		out.UnclassifiedCardCalls += unclassified
		// A reason belongs to the unpriced rows that recorded it. A priced row
		// with a stale reason is not a reason the money was unknown.
		if unpriced > 0 {
			if reason = strings.TrimSpace(reason); reason != "" {
				seen[reason] = true
			}
		}
	}
	if err := rows.Err(); err != nil {
		return Provenance{}, err
	}
	for reason := range seen {
		out.Reasons = append(out.Reasons, reason)
	}
	// Sorted, deduplicated by the GROUP BY: a repeated query returns the same
	// answer, and an unnamed reason stays out of the vocabulary rather than
	// entering it as an empty string.
	sort.Strings(out.Reasons)
	return out, nil
}
