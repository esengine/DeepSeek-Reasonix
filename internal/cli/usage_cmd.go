package cli

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"reasonix/internal/config"
	"reasonix/internal/event"
	usageutil "reasonix/internal/usage"
)

func usageCommand(args []string) int {
	fs := flag.NewFlagSet("usage", flag.ContinueOnError)
	days := fs.Int("days", 0, "show data for the last N days (0 = today)")
	provider := fs.String("provider", "", "filter by provider name")
	model := fs.String("model", "", "filter by model name")
	trend := fs.Bool("trend", false, "show daily token trend")
	tail := fs.Int("tail", 0, "show last N request details (0 = 50)")
	jsonOut := fs.Bool("json", false, "output as JSON")
	serve := fs.Bool("serve", false, "start web dashboard on localhost:9393")
	dir := fs.String("dir", "", "override usage store directory")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	storeDir := *dir
	if storeDir == "" {
		storeDir = filepath.Join(config.MemoryUserDir(), "usage")
	}

	if *serve {
		return runUsageDashboard(storeDir, *days)
	}

	if *jsonOut {
		return runUsageJSON(storeDir, *days, *provider, *model, *trend, *tail)
	}

	return runUsageText(storeDir, *days, *provider, *model, *trend, *tail)
}

func runUsageText(storeDir string, days int, provider, model string, trend bool, tail int) int {
	if trend {
		points, err := usageutil.QueryTrend(storeDir, days)
		if err != nil {
			fmt.Fprintln(os.Stderr, "query error:", err)
			return 1
		}
		label := fmt.Sprintf("Token Trend (last %d days)", days)
		if days <= 0 {
			label = "Token Trend (today)"
		}
		usageutil.PrintTrend(os.Stdout, points, label)
		return 0
	}

	if tail > 0 {
		entries, err := usageutil.QueryLogs(storeDir, tail, provider, model, 0)
		if err != nil {
			fmt.Fprintln(os.Stderr, "query error:", err)
			return 1
		}
		usageutil.PrintLogs(os.Stdout, entries, fmt.Sprintf("Last %d requests", tail))
		return 0
	}

	// Default: overview
	overview, err := usageutil.QueryOverview(storeDir, days)
	if err != nil {
		fmt.Fprintln(os.Stderr, "query error:", err)
		return 1
	}
	models, err := usageutil.QueryModels(storeDir, days)
	if err != nil {
		fmt.Fprintln(os.Stderr, "query error:", err)
		return 1
	}
	label := fmt.Sprintf("Usage (last %d days)", days)
	if days <= 0 {
		label = "Usage (today)"
	}
	usageutil.PrintOverview(os.Stdout, overview, models, label)
	return 0
}

func runUsageJSON(storeDir string, days int, provider, model string, trend bool, tail int) int {
	var data any
	var err error

	switch {
	case trend:
		data, err = usageutil.QueryTrend(storeDir, days)
	case tail > 0:
		data, err = usageutil.QueryLogs(storeDir, tail, provider, model, 0)
	default:
		type overviewResult struct {
			Overview usageutil.Overview  `json:"overview"`
			Models   []usageutil.ModelRow `json:"models"`
		}
		ov, e1 := usageutil.QueryOverview(storeDir, days)
		md, e2 := usageutil.QueryModels(storeDir, days)
		if e1 != nil {
			err = e1
		} else if e2 != nil {
			err = e2
		} else {
			data = overviewResult{Overview: ov, Models: md}
		}
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "query error:", err)
		return 1
	}
	if err := usageutil.PrintJSON(os.Stdout, data); err != nil {
		fmt.Fprintln(os.Stderr, "json error:", err)
		return 1
	}
	return 0
}

func runUsageDashboard(storeDir string, defaultDays int) int {
	addr := "127.0.0.1:9393"
	handler := usageutil.NewDashboardHandler(storeDir, defaultDays)
	fmt.Fprintf(os.Stderr, "Reasonix Usage Dashboard listening on http://%s\n", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		fmt.Fprintln(os.Stderr, "dashboard error:", err)
		return 1
	}
	return 0
}

// setupUsageTracker creates a usage store and wraps the sink with a usage sink.
// Returns the store (for cleanup) and the wrapped sink.
func setupUsageTracker(sink event.Sink) (*usageutil.Store, event.Sink) {
	usageDir := filepath.Join(config.MemoryUserDir(), "usage")
	store, err := usageutil.Open(usageDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "usage tracking disabled:", err)
		return nil, sink
	}
	return store, usageutil.NewSink(sink, store)
}

