package cli

// `reasonix doctor usage` audits a usage range read-only. These guards pin the
// exact text for a clean range, a range with one unpriced call, and rows that
// predate the card field, plus the JSON and the window rules.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

// usageProvenanceDir isolates every path the command resolves and returns the
// stats directory its fixtures are written under.
func usageProvenanceDir(t *testing.T) string {
	t.Helper()
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	state := testenv.TempDir(t)
	t.Setenv("REASONIX_STATE_HOME", state)
	t.Setenv("REASONIX_CACHE_HOME", testenv.TempDir(t))
	dir := filepath.Join(state, "stats")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeUsageProvenanceDay(t *testing.T, dir, day string, lines ...string) {
	t.Helper()
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, day+".jsonl"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// runUsageProvenance runs the subcommand and returns stdout, failing on a
// non-zero exit so a failed audit is not mistaken for an empty one.
func runUsageProvenance(t *testing.T, args ...string) string {
	t.Helper()
	var code int
	out, errOut := captureCLIOutput(t, func() {
		code = doctorCommand(append([]string{"usage"}, args...), "test-version")
	})
	if code != 0 {
		t.Fatalf("doctor usage exit = %d, stderr:\n%s", code, errOut)
	}
	return out
}

const provenanceHeader = "Usage provenance — 2026-08-10..2026-08-10 · source: all · currency: auto\n"

var provenanceCounted = "calls counted: 2 — the denominator: usage rows that carried tokens or a price fact\n"

// A fully priced, fully measured range says so plainly: every count is a
// measured zero, and the money is a bill.
func TestDoctorUsageAuditOfAFullyMeasuredRange(t *testing.T) {
	dir := usageProvenanceDir(t)
	writeUsageProvenanceDay(t, dir, "2026-08-10",
		`{"ts":"2026-08-10T01:00:00Z","model":"deepseek/x","source":"cli","total":100,"requests":1,"cost_amount":"1.50","cost_currency":"USD","cost_complete":true,"pricing_fingerprint":"card-aaaa"}`,
		`{"ts":"2026-08-10T02:00:00Z","model":"deepseek/x","source":"cli","total":100,"requests":1,"cost_amount":"2.50","cost_currency":"USD","cost_complete":true,"pricing_fingerprint":"card-bbbb"}`,
	)
	want := provenanceHeader + provenanceCounted +
		"calls unpriced: 0 of 2 (measured)\n" +
		"calls priced from an estimated/fallback card: 0 of 2 (measured)\n" +
		"calls whose rate card is not named: 0 of 2 (measured)\n" +
		"money: 4 USD — a bill: all 2 counted calls were priced\n"
	if got := runUsageProvenance(t, "--from", "2026-08-10", "--to", "2026-08-10"); got != want {
		t.Fatalf("doctor usage =\n%s\nwant\n%s", got, want)
	}
}

// The gap this command closes: the total already said the money was a floor
// and named the reason, but it could not say what the figure was over, and it
// had no line for an estimated card or an unnamed one. The audit answers all
// four questions, and the unknown makes the money a floor.
func TestDoctorUsageAuditOfARangeWithOneUnpricedCall(t *testing.T) {
	dir := usageProvenanceDir(t)
	writeUsageProvenanceDay(t, dir, "2026-08-10",
		`{"ts":"2026-08-10T01:00:00Z","model":"deepseek/x","source":"cli","total":100,"requests":1,"cost_amount":"1.50","cost_currency":"USD","cost_complete":true,"pricing_fingerprint":"card-aaaa"}`,
		`{"ts":"2026-08-10T02:00:00Z","model":"relay/new","source":"cli","total":50,"requests":1,"cost_amount":"0","cost_currency":"USD","cost_complete":false,"incomplete_reason":"missing_price_or_usage","pricing_fingerprint":"card-bbbb"}`,
	)
	want := provenanceHeader + provenanceCounted +
		"calls unpriced: 1 of 2 — their money is unknown\n" +
		"  reasons: missing_price_or_usage\n" +
		"calls priced from an estimated/fallback card: 0 of 2 (measured)\n" +
		"calls whose rate card is not named: 0 of 2 (measured)\n" +
		"money: 1.5 USD — a floor, not a bill: 1 of 2 counted calls have no price, so the true cost is at least this\n"
	if got := runUsageProvenance(t, "--from", "2026-08-10", "--to", "2026-08-10"); got != want {
		t.Fatalf("doctor usage =\n%s\nwant\n%s", got, want)
	}
}

// Rows written before the card field existed cannot be classified. The card
// answer is unknown with the row count it could not classify, never a zero
// that would read as "every card is named".
func TestDoctorUsageAuditOfRowsThatPredateTheCardField(t *testing.T) {
	dir := usageProvenanceDir(t)
	writeUsageProvenanceDay(t, dir, "2026-08-10",
		`{"ts":"2026-08-10T01:00:00Z","model":"deepseek/x","source":"cli","total":100,"requests":1}`,
		`{"ts":"2026-08-10T02:00:00Z","model":"relay/new","source":"cli","total":50,"requests":1}`,
	)
	want := provenanceHeader + provenanceCounted +
		"calls unpriced: 2 of 2 — their money is unknown\n" +
		"  reasons: no_price\n" +
		"calls priced from an estimated/fallback card: 0 of 2 (measured)\n" +
		"calls whose rate card is not named: unknown — 2 of 2 counted calls record no price fact, so whether their card was named was never recorded (not 0)\n" +
		"money: none recorded — a floor, not a bill: 2 of 2 counted calls have no price, so the true cost is at least this\n"
	if got := runUsageProvenance(t, "--from", "2026-08-10", "--to", "2026-08-10"); got != want {
		t.Fatalf("doctor usage =\n%s\nwant\n%s", got, want)
	}
}

// A price from a fallback card and a price whose card is unnamed are two
// different caveats on a bill; the audit names both counts.
func TestDoctorUsageAuditNamesEstimatedAndUnnamedCards(t *testing.T) {
	dir := usageProvenanceDir(t)
	writeUsageProvenanceDay(t, dir, "2026-08-10",
		`{"ts":"2026-08-10T01:00:00Z","model":"deepseek/x","source":"cli","total":100,"requests":1,"cost_amount":"1","cost_currency":"USD","cost_complete":true,"cost_estimated":true,"pricing_fingerprint":"card-aaaa"}`,
		`{"ts":"2026-08-10T02:00:00Z","model":"deepseek/x","source":"cli","total":100,"requests":1,"cost_amount":"2","cost_currency":"USD","cost_complete":true,"pricing_fingerprint":"card-bbbb"}`,
		`{"ts":"2026-08-10T03:00:00Z","model":"relay/new","source":"cli","total":100,"requests":1,"cost_amount":"0.5","cost_currency":"USD","cost_complete":true}`,
	)
	want := "Usage provenance — 2026-08-10..2026-08-10 · source: all · currency: auto\n" +
		"calls counted: 3 — the denominator: usage rows that carried tokens or a price fact\n" +
		"calls unpriced: 0 of 3 (measured)\n" +
		"calls priced from an estimated/fallback card: 1 of 3\n" +
		"calls whose rate card is not named: 1 of 3\n" +
		"money: 3.5 USD — a bill, partly estimated: 1 of 3 counted calls were priced from a fallback rate card\n"
	if got := runUsageProvenance(t, "--from", "2026-08-10", "--to", "2026-08-10"); got != want {
		t.Fatalf("doctor usage =\n%s\nwant\n%s", got, want)
	}
}

// provenanceJSON is the payload a script reads, with the fields the text
// prints; the card count is a pointer so an unknown is distinguishable from a
// measured zero.
type provenanceJSON struct {
	From                  string   `json:"from"`
	To                    string   `json:"to"`
	Source                string   `json:"source"`
	Currency              string   `json:"currency"`
	CountedCalls          int      `json:"countedCalls"`
	UnpricedCalls         int      `json:"unpricedCalls"`
	IncompleteReasons     []string `json:"incompleteReasons"`
	EstimatedCalls        int      `json:"estimatedCalls"`
	UnnamedCardCalls      *int     `json:"unnamedCardCalls"`
	UnclassifiedCardCalls int      `json:"unclassifiedCardCalls"`
	Cost                  []struct {
		Amount   string `json:"amount"`
		Currency string `json:"currency"`
	} `json:"cost"`
	Money string `json:"money"`
}

func decodeProvenance(t *testing.T, out string) provenanceJSON {
	t.Helper()
	var got provenanceJSON
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("doctor usage --json is not JSON: %v\n%s", err, out)
	}
	return got
}

func TestDoctorUsageAuditJSONCarriesTheSameNumbers(t *testing.T) {
	dir := usageProvenanceDir(t)
	writeUsageProvenanceDay(t, dir, "2026-08-10",
		`{"ts":"2026-08-10T01:00:00Z","model":"deepseek/x","source":"cli","total":100,"requests":1,"cost_amount":"1.50","cost_currency":"USD","cost_complete":true,"pricing_fingerprint":"card-aaaa"}`,
		`{"ts":"2026-08-10T02:00:00Z","model":"deepseek/x","source":"cli","total":100,"requests":1,"cost_amount":"2.50","cost_currency":"USD","cost_complete":true,"pricing_fingerprint":"card-bbbb"}`,
	)
	got := decodeProvenance(t, runUsageProvenance(t, "--from", "2026-08-10", "--to", "2026-08-10", "--json"))
	if got.From != "2026-08-10" || got.To != "2026-08-10" || got.Source != "all" || got.Currency != "auto" {
		t.Fatalf("range = %+v", got)
	}
	if got.CountedCalls != 2 || got.UnpricedCalls != 0 || got.EstimatedCalls != 0 ||
		got.UnclassifiedCardCalls != 0 || got.UnnamedCardCalls == nil || *got.UnnamedCardCalls != 0 {
		t.Fatalf("counts = %+v, want 2 counted and every unknown measured zero", got)
	}
	if len(got.IncompleteReasons) != 0 || len(got.Cost) != 1 || got.Cost[0].Amount != "4" || got.Cost[0].Currency != "USD" {
		t.Fatalf("reasons/cost = %+v, want no reasons and 4 USD", got)
	}
	if got.Money != "a bill: all 2 counted calls were priced" {
		t.Fatalf("money = %q", got.Money)
	}
}

// The unknown case: the JSON has to carry null, so a script cannot read the
// classified subset as the answer.
func TestDoctorUsageAuditJSONSaysUnknownForUnclassifiableRows(t *testing.T) {
	dir := usageProvenanceDir(t)
	writeUsageProvenanceDay(t, dir, "2026-08-10",
		`{"ts":"2026-08-10T01:00:00Z","model":"deepseek/x","source":"cli","total":100,"requests":1}`,
	)
	got := decodeProvenance(t, runUsageProvenance(t, "--from", "2026-08-10", "--to", "2026-08-10", "--json"))
	if got.UnnamedCardCalls != nil {
		t.Fatalf("unnamedCardCalls = %d, want null for a row that cannot be classified", *got.UnnamedCardCalls)
	}
	if got.UnclassifiedCardCalls != 1 || got.CountedCalls != 1 {
		t.Fatalf("counts = %+v, want 1 counted and 1 unclassified", got)
	}
}

// The source filter is the usage page's: the header names the selection and
// rows from another source stay out.
func TestDoctorUsageAuditKeepsTheUsagePagesSourceFilter(t *testing.T) {
	dir := usageProvenanceDir(t)
	writeUsageProvenanceDay(t, dir, "2026-08-10",
		`{"ts":"2026-08-10T01:00:00Z","model":"deepseek/x","source":"cli","total":100,"requests":1,"cost_amount":"1.50","cost_currency":"USD","cost_complete":true,"pricing_fingerprint":"card-aaaa"}`,
		`{"ts":"2026-08-10T02:00:00Z","model":"deepseek/x","source":"desktop","total":200,"requests":1,"cost_amount":"2.50","cost_currency":"USD","cost_complete":true,"pricing_fingerprint":"card-bbbb"}`,
	)
	got := runUsageProvenance(t, "--from", "2026-08-10", "--to", "2026-08-10", "--source", "cli")
	if !strings.HasPrefix(got, "Usage provenance — 2026-08-10..2026-08-10 · source: cli · currency: auto\n") {
		t.Fatalf("header did not name the source filter:\n%s", got)
	}
	if !strings.Contains(got, "calls counted: 1 ") || !strings.Contains(got, "money: 1.5 USD") {
		t.Fatalf("desktop row was not filtered out:\n%s", got)
	}
}

// The window rules mirror the usage page: a trailing day count or an explicit
// inclusive range, never both, capped at a year.
func TestUsageProvenanceWindowRules(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.Local)
	day := func(f interface{ Format(string) string }) string { return f.Format("2006-01-02") }
	check := func(name string, days int, from, to string, daysSet bool, wantFrom, wantTo, wantErr string) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			got, err := usageProvenanceFilter(days, from, to, now, daysSet)
			if wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), wantErr) {
					t.Fatalf("err = %v, want one naming %q", err, wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if gotFrom, gotTo := day(got.From), day(got.To); gotFrom != wantFrom || gotTo != wantTo {
				t.Fatalf("window = %s..%s, want %s..%s", gotFrom, gotTo, wantFrom, wantTo)
			}
		})
	}
	check("explicit range", 30, "2026-08-01", "2026-08-03", false, "2026-08-01", "2026-08-03", "")
	check("trailing days", 7, "", "", false, "2026-08-04", "2026-08-10", "")
	check("one day", 1, "", "", false, "2026-08-10", "2026-08-10", "")
	check("from without to", 30, "2026-08-01", "", false, "", "", "must be provided together")
	check("days with range", 7, "2026-08-01", "2026-08-03", true, "", "", "cannot be combined")
	check("reversed", 30, "2026-08-03", "2026-08-01", false, "", "", "between 1 and 365")
	check("too long", 30, "2025-08-01", "2026-08-10", false, "", "", "between 1 and 365")
	check("bad from", 30, "08/01/2026", "2026-08-10", false, "", "", "--from must be YYYY-MM-DD")
	check("zero days", 0, "", "", false, "", "", "--days must be between 1 and 365")
	check("too many days", 366, "", "", false, "", "", "--days must be between 1 and 365")
}

func TestDoctorUsageRejectsAPositionalArgument(t *testing.T) {
	usageProvenanceDir(t)
	var code int
	_, stderr := captureCLIOutput(t, func() {
		code = doctorCommand([]string{"usage", "extra"}, "test-version")
	})
	if code != 2 || !strings.Contains(stderr, "usage: reasonix doctor usage") {
		t.Fatalf("exit = %d, stderr = %q, want a usage error", code, stderr)
	}
}
