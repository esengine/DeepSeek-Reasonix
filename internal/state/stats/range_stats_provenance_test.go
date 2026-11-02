package stats

// RangeStats is answered by two read paths: the exact JSONL read (aggregateDays)
// and the projection (rangeStatsFromCatalog -> rangeStatsFromRollups). This guard
// makes every field say which of them fills it, and checks that claim.

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

// The two answers a field can declare: both read paths fill it, or only the
// exact JSONL read does.
const (
	answeredByBoth      = "both"
	answeredByExactOnly = "exact only"
)

// rangeStatsField is one line of rangeStatsProvenance.
type rangeStatsField struct {
	// paths is answeredByBoth or answeredByExactOnly.
	paths string
	// optional, on a "both" field, names when a zero value is a real answer
	// rather than a path that forgot to fill it.
	optional string
	// reason, on an "exact only" field, is why the projection cannot answer it.
	reason string
	// consequence, on an "exact only" field, is what a caller reading a
	// projection answer sees instead.
	consequence string
}

// rangeStatsProvenance declares, one line per field, which read paths answer
// each part of RangeStats. A field declared "both" must be non-zero in the
// projection result of provenanceFixture; an "exact only" field must stay zero
// there. Adding a field to RangeStats without adding a line here fails
// TestEveryAggregateFieldDeclaresItsProvenance.
var rangeStatsProvenance = map[string]rangeStatsField{
	// Range bounds are stamped from the filter by both paths.
	"From": {paths: answeredByBoth},
	"To":   {paths: answeredByBoth},

	// Totals: both paths sum the same stored rows.
	"Tokens":    {paths: answeredByBoth},
	"Requests":  {paths: answeredByBoth},
	"Turns":     {paths: answeredByBoth},
	"CacheHit":  {paths: answeredByBoth},
	"CacheMiss": {paths: answeredByBoth},

	// Money and price caveats: the rollups carry them, but a range without
	// such a fact legitimately answers zero/nil on both paths.
	"Cost":              {paths: answeredByBoth, optional: "nil when no row in range carried a price"},
	"EstimateCost":      {paths: answeredByBoth, optional: "nil when the range quoted no subscription-equivalent usage"},
	"CostEstimated":     {paths: answeredByBoth, optional: "false when no row folded in a fallback rate card"},
	"CostIncomplete":    {paths: answeredByBoth, optional: "false when every counted call is priced"},
	"UnpricedCalls":     {paths: answeredByBoth, optional: "0 when every counted call is priced"},
	"IncompleteReasons": {paths: answeredByBoth, optional: "nil when no unpriced call recorded a reason"},

	// Rolled-up views: both paths derive them from the same rows.
	"ActiveDays":  {paths: answeredByBoth},
	"TopModel":    {paths: answeredByBoth, optional: "empty when the range counted no usage"},
	"TopProvider": {paths: answeredByBoth, optional: "empty when the range counted no usage"},
	"Daily":       {paths: answeredByBoth},
	"Models":      {paths: answeredByBoth},
	"Providers":   {paths: answeredByBoth},

	// A rollup is per day: only the exact read's single-day query buckets by
	// hour, and a one-day range never consults the catalog.
	"Hourly": {
		paths:       answeredByExactOnly,
		reason:      "the day rollup has no hour column, and Query sends a one-day range to the exact read",
		consequence: "a projection answer has no hourly series, so an hourly view must keep taking the exact read",
	},
}

// provenanceShapeProblems checks the declaration's shape: every field of typ is
// named, every name still exists, and every "exact only" line explains itself.
// It is factored out so the demonstration test can drive it over a local struct
// instead of deleting and re-adding a real field.
func provenanceShapeProblems(typ reflect.Type, declared map[string]rangeStatsField, projection reflect.Value) []string {
	const where = "rangeStatsProvenance (internal/state/stats/range_stats_provenance_test.go)"
	var problems []string
	for field := range typ.Fields() {
		decl, ok := declared[field.Name]
		if !ok {
			filled := projection.IsValid() && !projection.FieldByName(field.Name).IsZero()
			state := "the projection leaves it zero: if it is truly filled by both paths, extend provenanceFixture until the projection fills it; if only the exact read fills it, declare \"exact only\" and give the reason and consequence"
			if filled {
				state = "the projection already fills it: declare \"both\""
			}
			problems = append(problems, fmt.Sprintf(
				"%s.%s has no provenance declaration: add a line to %s saying whether both read paths fill it (\"both\") or only the exact read (\"exact only\", with a reason and a consequence). In the provenance fixture %s.",
				typ.Name(), field.Name, where, state))
			continue
		}
		switch decl.paths {
		case answeredByBoth:
		case answeredByExactOnly:
			if decl.reason == "" || decl.consequence == "" {
				problems = append(problems, fmt.Sprintf(
					"%s entry for %q declares \"exact only\" but has no %s: tell the next reader why the projection cannot answer it and what a projection answer shows instead.",
					where, field.Name, missingParts(decl.reason, decl.consequence)))
			}
		default:
			problems = append(problems, fmt.Sprintf(
				"%s entry for %q names paths %q; use answeredByBoth or answeredByExactOnly.",
				where, field.Name, decl.paths))
		}
	}
	for name := range declared {
		if _, ok := typ.FieldByName(name); !ok {
			problems = append(problems, fmt.Sprintf(
				"%s names %q, but %s has no such field: remove the stale line, or restore the field it describes.",
				where, name, typ.Name()))
		}
	}
	sort.Strings(problems)
	return problems
}

// provenanceRealityProblems checks the declaration against the fixture, using
// the declaration itself to decide which path must fill which field. There is no
// second list: a "both" line is checked for a non-zero projection and a non-zero
// exact read, an "exact only" line for a zero projection and for a fixture read
// that proves the field is alive.
func provenanceRealityProblems(typ reflect.Type, declared map[string]rangeStatsField, exact, projection, singleDay reflect.Value) []string {
	const where = "rangeStatsProvenance (internal/state/stats/range_stats_provenance_test.go)"
	var problems []string
	for field := range typ.Fields() {
		name := field.Name
		decl, ok := declared[name]
		if !ok {
			continue // provenanceShapeProblems already reported the missing line.
		}
		inExact := !exact.FieldByName(name).IsZero()
		inProjection := !projection.FieldByName(name).IsZero()
		switch decl.paths {
		case answeredByBoth:
			if !inProjection {
				problems = append(problems, fmt.Sprintf(
					"%s.%s is declared answered by both read paths in %s, but the projection left it zero: either the projection stopped filling it (fix the projection) or it is exact-only (correct the declaration).",
					typ.Name(), name, where))
			}
			if !inExact {
				problems = append(problems, fmt.Sprintf(
					"%s.%s is declared answered by both read paths in %s, but the exact JSONL read left it zero: the fixture no longer exercises it, so extend provenanceFixture or correct the declaration.",
					typ.Name(), name, where))
			}
		case answeredByExactOnly:
			if inProjection {
				problems = append(problems, fmt.Sprintf(
					"%s.%s is declared exact-only in %s, but the projection filled it: if the projection now answers it, update the declaration and its reason/consequence; otherwise the projection wrongly duplicates the exact read.",
					typ.Name(), name, where))
			}
			if !inExact && singleDay.FieldByName(name).IsZero() {
				problems = append(problems, fmt.Sprintf(
					"%s.%s is declared exact-only in %s, but no fixture read fills it: the declaration would hide a dead field, so extend provenanceFixture or correct the declaration.",
					typ.Name(), name, where))
			}
		}
	}
	sort.Strings(problems)
	return problems
}

// provenanceFixture returns the exact read, a projection read and a one-day
// exact read of one two-day history. The history exercises every field the
// declaration says both paths answer: a billed fallback-priced call, a
// subscription-equivalent estimate, a completed turn and an unpriced call.
func provenanceFixture(t *testing.T) (exact, projection, singleDay RangeStats) {
	t.Helper()
	dir := testenv.TempDir(t)
	writeDay(t, dir, "2026-09-01",
		map[string]any{"ts": "2026-09-01T01:00:00Z", "model": "deepseek/deepseek-chat", "source": "cli",
			"prompt": 80, "completion": 20, "total": 100, "requests": 1,
			"cache_hit": 60, "cache_miss": 40,
			"cost_amount": "1.50", "cost_currency": "CNY", "cost_complete": true,
			"cost_estimated": true, "billing_mode": "payg"},
		map[string]any{"ts": "2026-09-01T02:00:00Z", "model": "relay/plan", "source": "cli",
			"total": 50, "requests": 1,
			"cost_amount": "0.30", "cost_currency": "USD", "cost_complete": true,
			"billing_mode": "subscription_equivalent"},
		map[string]any{"ts": "2026-09-01T03:00:00Z", "source": "cli", "turn": true},
	)
	writeDay(t, dir, "2026-09-02",
		map[string]any{"ts": "2026-09-02T01:00:00Z", "model": "relay/new-model", "source": "cli",
			"total": 40, "requests": 1, "cost_complete": false, "incomplete_reason": "missing_price_or_usage"},
	)
	var err error
	exact, err = NewWriter(dir).Query(rangeFromDays(t, "2026-09-01", "2026-09-03"))
	if err != nil {
		t.Fatal(err)
	}
	projection = queryProjection(t, dir, rangeFromDays(t, "2026-09-01", "2026-09-03"))
	singleDay, err = NewWriter(dir).Query(rangeFromDays(t, "2026-09-01", "2026-09-01"))
	if err != nil {
		t.Fatal(err)
	}
	return exact, projection, singleDay
}

// TestEveryAggregateFieldDeclaresItsProvenance is the control: the committed
// RangeStats, its current declaration and the fixture agree. A new field, a
// deleted field or a declaration that no longer matches a read path fails here
// with the field named.
func TestEveryAggregateFieldDeclaresItsProvenance(t *testing.T) {
	typ := reflect.TypeFor[RangeStats]()
	exact, projection, singleDay := provenanceFixture(t)
	if problems := provenanceShapeProblems(typ, rangeStatsProvenance, reflect.ValueOf(projection)); len(problems) > 0 {
		t.Fatalf("the committed RangeStats does not pass its own provenance guard:\n%s", strings.Join(problems, "\n"))
	}
	if problems := provenanceRealityProblems(typ, rangeStatsProvenance, reflect.ValueOf(exact), reflect.ValueOf(projection), reflect.ValueOf(singleDay)); len(problems) > 0 {
		t.Fatalf("the provenance declaration disagrees with the read paths:\n%s", strings.Join(problems, "\n"))
	}
}

// TestTheProvenanceGuardCatchesAnUndeclaredField proves the guard fires without
// deleting a field from RangeStats: the local struct is a new field on top of the
// same contract, and provenanceShapeProblems is the seam the guard uses.
func TestTheProvenanceGuardCatchesAnUndeclaredField(t *testing.T) {
	type aggregateWithAnUndeclaredField struct {
		From   string
		Tokens int64
		Hourly []HourlyTokens
	}
	declared := map[string]rangeStatsField{
		"From":   {paths: answeredByBoth},
		"Tokens": {paths: answeredByBoth},
		// Hourly was added to the struct without a line here.
	}
	projection := reflect.ValueOf(aggregateWithAnUndeclaredField{From: "2026-09-01", Tokens: 100})
	problems := provenanceShapeProblems(reflect.TypeFor[aggregateWithAnUndeclaredField](), declared, projection)
	if len(problems) != 1 {
		t.Fatalf("guard reported %d problems, want exactly the one undeclared field:\n%s",
			len(problems), strings.Join(problems, "\n"))
	}
	for _, want := range []string{"aggregateWithAnUndeclaredField.Hourly", "rangeStatsProvenance", "the projection leaves it zero"} {
		if !strings.Contains(problems[0], want) {
			t.Errorf("the undeclared-field message does not contain %q: %s", want, problems[0])
		}
	}
}

// A stale line is the other half of the shape guard: a declaration that names a
// field the struct no longer has must not linger as an unread claim.
func TestTheProvenanceGuardCatchesAStaleEntry(t *testing.T) {
	type aggregateWithoutTheStaleField struct {
		From string
	}
	declared := map[string]rangeStatsField{
		"From":   {paths: answeredByBoth},
		"Tokens": {paths: answeredByBoth},
	}
	problems := provenanceShapeProblems(reflect.TypeFor[aggregateWithoutTheStaleField](), declared, reflect.ValueOf(aggregateWithoutTheStaleField{From: "2026-09-01"}))
	if len(problems) != 1 {
		t.Fatalf("guard reported %d problems, want exactly the one stale entry:\n%s",
			len(problems), strings.Join(problems, "\n"))
	}
	for _, want := range []string{`"Tokens"`, "has no such field", "remove the stale line"} {
		if !strings.Contains(problems[0], want) {
			t.Errorf("the stale-entry message does not contain %q: %s", want, problems[0])
		}
	}
}

func missingParts(reason, consequence string) string {
	switch {
	case reason == "" && consequence == "":
		return "reason or consequence"
	case reason == "":
		return "reason"
	default:
		return "consequence"
	}
}
