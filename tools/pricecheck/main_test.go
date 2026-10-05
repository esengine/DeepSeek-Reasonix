package main

import (
	"os"
	"reasonix/internal/contract/pricing"
	"strings"
	"testing"
)

func tableFrom(t *testing.T, path string) table {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	tables, err := parseTables(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) == 0 {
		t.Fatalf("%s holds no table", path)
	}
	return tables[0]
}

// The numbers are the ones the saved pages state. They are asserted literally
// rather than against billing's table: a fixture is a snapshot of what a vendor
// published, and comparing it to today's rates is what pricecheck does live.
func TestReadersTakeTheRatesThePagesState(t *testing.T) {
	for _, tc := range []struct {
		name   string
		path   string
		models []string
		read   func(table, []string) ([]published, error)
		want   []published
	}{
		{
			name: "deepseek prices two models off-peak", path: "testdata/deepseek-usd.html",
			models: []string{"deepseek-flash", "deepseek-v4-pro"}, read: readDeepSeek,
			want: []published{
				{Model: "deepseek-flash", Rate: card(0.003, 0.15, 0.6)},
				{Model: "deepseek-v4-pro", Rate: card(0.022, 0.66, 1.98)},
			},
		},
		{
			name: "longcat states the discounted column last", path: "testdata/longcat-usd.html",
			models: []string{"LongCat-2.0"}, read: readLongCat,
			want: []published{{Model: "LongCat-2.0", Rate: card(0.006, 0.30, 1.20)}},
		},
		{
			name: "longcat in the other language", path: "testdata/longcat-cny.html",
			models: []string{"LongCat-2.0"}, read: readLongCat,
			want: []published{{Model: "LongCat-2.0", Rate: card(0.04, 2, 8)}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.read(tableFrom(t, tc.path), tc.models)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("read %d rates, want %d", len(got), len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("rate %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// "Cached Input" is a substring of "Uncached Input". A reader that matches on
// containment takes the uncached rate for both and reports agreement on a
// number the vendor never published for that line.
func TestCachedAndUncachedRowsAreNotConfused(t *testing.T) {
	got, err := readLongCat(tableFrom(t, "testdata/longcat-usd.html"), []string{"LongCat-2.0"})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Rate.CacheHit == got[0].Rate.Input {
		t.Fatalf("cache-hit and input read the same row: %+v", got[0].Rate)
	}
}

// Every way of failing to read a page has to be an error. A reader that returns
// zeroes turns "the page changed" into "the vendor now charges nothing", and a
// checker that reports agreement on that is worse than having none.
func TestUnreadablePagesAreErrorsAndNotZeroes(t *testing.T) {
	for _, tc := range []struct {
		name string
		html string
		read func(table, []string) ([]published, error)
	}{
		{"deepseek without a model row", `<table><tr><td>Price</td><td>$1</td></tr></table>`, readDeepSeek},
		{"deepseek naming other models", `<table><tr><td>deepseek-flash</td></tr>
			<tr><td>cache hit</td><td>off-peak</td><td>$1</td></tr></table>`, readDeepSeek},
		{"longcat with a renamed row", `<table><tr><td>Input</td><td>$1</td></tr>
			<tr><td>Output</td><td>$2</td></tr></table>`, readLongCat},
		{"longcat with no amount", `<table><tr><td>Cached Input</td><td>free</td></tr>
			<tr><td>Uncached Input</td><td>free</td></tr><tr><td>Output</td><td>free</td></tr></table>`, readLongCat},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tables, err := parseTables(strings.NewReader(tc.html))
			if err != nil {
				t.Fatal(err)
			}
			models := []string{"deepseek-flash", "deepseek-v4-pro"}
			if tc.read == nil || len(tables) == 0 {
				t.Fatal("fixture did not parse")
			}
			if strings.Contains(tc.name, "longcat") {
				models = []string{"LongCat-2.0"}
			}
			if got, err := tc.read(tables[0], models); err == nil {
				t.Fatalf("read succeeded on an unreadable page: %+v", got)
			}
		})
	}
}

// Two rows carrying the same marker mean the page grew a section this reader
// cannot tell apart; picking either is guessing.
func TestAmbiguousRowsDoNotResolve(t *testing.T) {
	tables, _ := parseTables(strings.NewReader(
		`<table><tr><td>Output</td><td>$1</td></tr><tr><td>Output</td><td>$2</td></tr></table>`))
	if _, ok := tables[0].rowWithCell("Output"); ok {
		t.Fatal("an ambiguous page resolved to one row")
	}
	if _, ok := tables[0].rowContaining("output"); ok {
		t.Fatal("an ambiguous page resolved to one row")
	}
}

func card(cacheHit, input, output float64) pricing.RateCard {
	return pricing.RateCard{CacheHit: cacheHit, Input: input, Output: output}
}
