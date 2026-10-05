// pricecheck compares the rates in billing's table against what each vendor
// publishes today, and reports how long ago a person last confirmed them.
//
// It reports, it does not gate: it reaches the network, and a page changing
// shape is something to look at rather than a build to fail. What it must never
// do is answer "agrees" for a page it could not read — that is the one failure
// mode a price checker cannot have, so unread and unreadable are their own
// verdicts and neither counts as agreement.
package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reasonix/internal/contract/pricing"
	"slices"
	"strings"
	"time"
)

type verdict string

const (
	agrees     verdict = "agrees"
	differs    verdict = "DIFFERS"
	unread     verdict = "unread"
	unreadable verdict = "UNREADABLE"
)

func main() {
	timeout := flag.Duration("timeout", 30*time.Second, "per-page fetch timeout")
	offline := flag.String("offline", "", "read pages from this directory instead of the network")
	flag.Parse()

	failed := false
	for _, s := range sources() {
		fmt.Printf("%s/%s  %s\n", s.Provider, s.Currency, s.URL)
		if s.Read == nil {
			fmt.Printf("    %-11s %s\n", unread, s.Unread)
			continue
		}
		rates, err := readSource(s, *offline, *timeout)
		if err != nil {
			fmt.Printf("    %-11s %v\n", unreadable, err)
			failed = true
			continue
		}
		for _, got := range rates {
			if reportModel(s, got) == differs {
				failed = true
			}
		}
	}
	reportCheckedOn()
	if failed {
		fmt.Fprintln(os.Stderr, "\npricecheck: a page disagrees with the table or could not be read.")
		fmt.Fprintln(os.Stderr, "A price change is an append to officialRates, never an edit — the")
		fmt.Fprintln(os.Stderr, "superseded rate is what lets an installed config be brought forward.")
		os.Exit(1)
	}
}

func readSource(s source, offline string, timeout time.Duration) ([]published, error) {
	body, err := fetch(s, offline, timeout)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	tables, err := parseTables(body)
	if err != nil {
		return nil, err
	}
	if len(tables) == 0 {
		return nil, fmt.Errorf("the page states no table")
	}
	return s.Read(tables[0], s.Models)
}

func fetch(s source, offline string, timeout time.Duration) (io.ReadCloser, error) {
	if offline != "" {
		return os.Open(filepath.Join(offline, offlineName(s)))
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(s.URL)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return resp.Body, nil
}

// offlineName is the fixture a page is read from with -offline, and the name to
// save one under: the vendor, its currency, and nothing that can collide.
func offlineName(s source) string {
	return strings.ToLower(s.Provider + "-" + s.Currency + ".html")
}

func reportModel(s source, got published) verdict {
	want := pricing.CurrentRate(s.Provider, got.Model, s.Currency)
	if want == nil {
		fmt.Printf("    %-11s %s is priced by the page and by nothing local\n", differs, got.Model)
		return differs
	}
	if got.Rate.CacheHit == want.CacheHit && got.Rate.Input == want.Input && got.Rate.Output == want.Output {
		fmt.Printf("    %-11s %-18s %g / %g / %g\n", agrees, got.Model, want.CacheHit, want.Input, want.Output)
		return agrees
	}
	fmt.Printf("    %-11s %-18s page %g / %g / %g, table %g / %g / %g\n", differs, got.Model,
		got.Rate.CacheHit, got.Rate.Input, got.Rate.Output, want.CacheHit, want.Input, want.Output)
	return differs
}

// reportCheckedOn says how stale each vendor's last human confirmation is. No
// threshold: a number nobody chose is one nobody can defend, and the date is
// what a reader needs to decide whether to go and look.
func reportCheckedOn() {
	oldest := map[string]string{}
	for _, entry := range pricing.OfficialCatalog() {
		if current, seen := oldest[entry.Provider]; !seen || entry.CheckedOn < current {
			oldest[entry.Provider] = entry.CheckedOn
		}
	}
	providers := make([]string, 0, len(oldest))
	for provider := range oldest {
		providers = append(providers, provider)
	}
	slices.Sort(providers)
	fmt.Println("\nlast confirmed by a person:")
	for _, provider := range providers {
		fmt.Printf("    %-10s %s%s\n", provider, oldest[provider], ageOf(oldest[provider]))
	}
}

func ageOf(checkedOn string) string {
	when, err := time.Parse("2006-01-02", checkedOn)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("  (%d days ago)", int(time.Since(when).Hours()/24))
}
