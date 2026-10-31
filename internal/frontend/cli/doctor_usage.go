package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/frontend/doctor"
	"reasonix/internal/state/stats"
)

const (
	usageProvenanceDefaultDays = 30
	usageProvenanceMaxDays     = 365
	usageProvenanceDayLayout   = "2006-01-02"
)

// doctorUsageCommand audits one usage range in the vocabulary the aggregate
// already uses. It reads, and never re-prices or rewrites a call.
func doctorUsageCommand(args []string) int {
	fs := flag.NewFlagSet("doctor usage", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "print the provenance audit as JSON")
	days := fs.Int("days", usageProvenanceDefaultDays, "trailing days to audit (1-365)")
	from := fs.String("from", "", "first day (YYYY-MM-DD); requires --to")
	to := fs.String("to", "", "last day (YYYY-MM-DD); requires --from")
	source := fs.String("source", "", "usage source (desktop, cli, serve, bot, remote; empty is all)")
	currency := fs.String("currency", "", "display currency (auto, CNY, USD; empty reads the configured preference)")
	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}
	if fs.NArg() != 0 {
		usageProvenanceUsage()
		return 2
	}
	filter, err := usageProvenanceFilter(*days, *from, *to, time.Now(), flagVisited(fs, "days"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}
	filter.Source = strings.TrimSpace(*source)
	value := strings.TrimSpace(*currency)
	if value == "" {
		// The usage page reads money in this session's display currency; the
		// CLI has no session, so the same preference is read from config.
		if cfg, err := config.LoadForRootReadOnly("."); err == nil {
			value = cfg.DisplayCurrencyPref()
		}
	}
	pref, err := parseCLIPricingCurrency(value)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}
	filter.Currency = pref
	report, err := doctor.CollectUsageProvenance(filter)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	fmt.Print(doctor.RenderUsageProvenanceText(report))
	return 0
}

func usageProvenanceUsage() {
	fmt.Fprintln(os.Stderr, "usage: reasonix doctor usage [--days N | --from YYYY-MM-DD --to YYYY-MM-DD] [--source S] [--currency auto|CNY|USD] [--json]")
}

// flagVisited reports whether the command line set a flag, so a default can
// stay distinguishable from the user picking it.
func flagVisited(fs *flag.FlagSet, name string) bool {
	visited := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			visited = true
		}
	})
	return visited
}

// usageProvenanceFilter is the same window selection the usage page accepts:
// either a trailing day count or an explicit inclusive calendar range, never
// both, capped at a year.
func usageProvenanceFilter(days int, fromRaw, toRaw string, now time.Time, daysSet bool) (stats.SourceFilter, error) {
	fromRaw, toRaw = strings.TrimSpace(fromRaw), strings.TrimSpace(toRaw)
	if fromRaw != "" || toRaw != "" {
		if daysSet {
			return stats.SourceFilter{}, fmt.Errorf("--from/--to and --days cannot be combined")
		}
		if fromRaw == "" || toRaw == "" {
			return stats.SourceFilter{}, fmt.Errorf("--from and --to must be provided together")
		}
		from, err := time.Parse(usageProvenanceDayLayout, fromRaw)
		if err != nil {
			return stats.SourceFilter{}, fmt.Errorf("--from must be YYYY-MM-DD")
		}
		toDay, err := time.Parse(usageProvenanceDayLayout, toRaw)
		if err != nil {
			return stats.SourceFilter{}, fmt.Errorf("--to must be YYYY-MM-DD")
		}
		if toDay.Before(from) || from.Before(toDay.AddDate(0, 0, -(usageProvenanceMaxDays-1))) {
			return stats.SourceFilter{}, fmt.Errorf("range must be between 1 and %d days", usageProvenanceMaxDays)
		}
		// A date is a calendar day, not an instant; UTC only carries it and the
		// query formats the day strings.
		to := time.Date(toDay.Year(), toDay.Month(), toDay.Day(), 23, 59, 59, 0, time.UTC)
		return stats.SourceFilter{From: from, To: to}, nil
	}
	if days < 1 || days > usageProvenanceMaxDays {
		return stats.SourceFilter{}, fmt.Errorf("--days must be between 1 and %d", usageProvenanceMaxDays)
	}
	to := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, now.Location())
	return stats.SourceFilter{From: to.AddDate(0, 0, -(days - 1)), To: to}, nil
}
