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

// provenanceCounts is the one set of predicates the range total and the per-day
// series are counted from. Both are grouped reads over the flags the quote path
// already recorded, so a day cannot classify a call differently than the total
// above it.
const provenanceCounts = `SUM(CASE WHEN total>0 OR cost_currency<>'' OR cost_incomplete=1 THEN 1 ELSE 0 END),
    SUM(cost_incomplete),
    SUM(CASE WHEN cost_incomplete=0 AND cost_currency<>'' AND cost_estimated=1 THEN 1 ELSE 0 END),
    SUM(CASE WHEN cost_currency<>'' AND pricing_fingerprint='' THEN 1 ELSE 0 END),
    SUM(CASE WHEN cost_currency='' AND (total>0 OR cost_incomplete=1) THEN 1 ELSE 0 END)`

// provenanceWhere builds the row set both reads share: the range's days, the
// requested source, and no turn markers (a turn counts no usage, so it is not
// a call the money is over).
func provenanceWhere(fromDay, toDay, source string) (string, []any) {
	where := `day>=? AND day<=? AND turns=0`
	args := []any{fromDay, toDay}
	if source != "" && source != "all" {
		where += ` AND source=?`
		args = append(args, source)
	}
	return where, args
}

// add folds one grouped row into the tally.
func (p *Provenance) add(reason string, counted, unpriced, estimated, unnamed, unclassified int, reasons map[string]bool) {
	p.CountedCalls += counted
	p.UnpricedCalls += unpriced
	p.EstimatedCalls += estimated
	p.UnnamedCardCalls += unnamed
	p.UnclassifiedCardCalls += unclassified
	// A reason belongs to the unpriced rows that recorded it. A priced row
	// with a stale reason is not a reason the money was unknown.
	if unpriced > 0 {
		if reason = strings.TrimSpace(reason); reason != "" {
			reasons[reason] = true
		}
	}
}

func sortedReasons(seen map[string]bool) []string {
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for reason := range seen {
		out = append(out, reason)
	}
	// Sorted, deduplicated by the GROUP BY: a repeated query returns the same
	// answer, and an unnamed reason stays out of the vocabulary rather than
	// entering it as an empty string.
	sort.Strings(out)
	return out
}

// ProvenanceInRange reads the stored row flags. Every predicate is a flag the
// quote path already recorded, so this answers what the exact JSONL read
// answers; the reasons come from the same pass as the counts, not a second
// scan. The projection keeps sums, so the row count is read here rather than
// derived from a rollup.
func (c *Catalog) ProvenanceInRange(ctx context.Context, fromDay, toDay, source string) (Provenance, error) {
	total, _, err := c.ProvenanceByDay(ctx, fromDay, toDay, source)
	return total, err
}

// ProvenanceByDay answers the same accounting per calendar day, and the range
// total over those same rows. It is one read so a caller that draws a series
// under a total cannot straddle a reconcile: the day's count and the total are
// grouped from the identical row set.
func (c *Catalog) ProvenanceByDay(ctx context.Context, fromDay, toDay, source string) (Provenance, map[string]Provenance, error) {
	where, args := provenanceWhere(fromDay, toDay, source)
	rows, err := c.db.QueryContext(ctx, `SELECT day,incomplete_reason,`+provenanceCounts+`
        FROM usage_records WHERE `+where+` GROUP BY day,incomplete_reason ORDER BY day`, args...)
	if err != nil {
		return Provenance{}, nil, err
	}
	defer rows.Close()
	var total Provenance
	totalReasons := map[string]bool{}
	byDay := map[string]Provenance{}
	dayReasons := map[string]map[string]bool{}
	for rows.Next() {
		var day, reason string
		var counted, unpriced, estimated, unnamed, unclassified int
		if err := rows.Scan(&day, &reason, &counted, &unpriced, &estimated, &unnamed, &unclassified); err != nil {
			return Provenance{}, nil, err
		}
		seen := dayReasons[day]
		if seen == nil {
			seen = map[string]bool{}
			dayReasons[day] = seen
		}
		dayTally := byDay[day]
		dayTally.add(reason, counted, unpriced, estimated, unnamed, unclassified, seen)
		byDay[day] = dayTally
		// The total is folded from the same numbers, not queried again.
		total.add(reason, counted, unpriced, estimated, unnamed, unclassified, totalReasons)
	}
	if err := rows.Err(); err != nil {
		return Provenance{}, nil, err
	}
	total.Reasons = sortedReasons(totalReasons)
	for day, seen := range dayReasons {
		tally := byDay[day]
		tally.Reasons = sortedReasons(seen)
		byDay[day] = tally
	}
	return total, byDay, nil
}
