package main

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/stats"
)

// UsageStatsRequest asks for the usage statistics panel aggregate.
// Range days: 7 / 14 / 30 / 90, or a custom From/To pair (inclusive, local
// dates). Source "" or "all" aggregates every entry point (desktop, cli, serve,
// bot, remote); any other value filters to that source's records.
type UsageStatsRequest struct {
	Range  string `json:"range"`          // "7" | "14" | "30" | "90" | "custom"
	From   string `json:"from,omitempty"` // "2006-01-02", custom only
	To     string `json:"to,omitempty"`
	Source string `json:"source,omitempty"` // "" | "all" | "desktop" | "cli" | "serve" | "bot" | "remote"
}

// UsageStatsRange is the aggregate response. Fields map 1:1 to the settings
// panel sections (totals, derived stats, daily trend, per-model split).
type UsageStatsRange struct {
	From        string                `json:"from"`
	To          string                `json:"to"`
	Tokens      int64                 `json:"tokens"`
	Requests    int                   `json:"requests"`
	Turns       int                   `json:"turns"`
	CacheHit    int64                 `json:"cacheHit"`
	CacheMiss   int64                 `json:"cacheMiss"`
	ActiveDays  int                   `json:"activeDays"`
	TopModel    string                `json:"topModel"`
	TopProvider string                `json:"topProvider"`
	Daily       []stats.DailyTokens   `json:"daily"`
	Models      []stats.ModelUsage    `json:"models"`
	Providers   []stats.ProviderUsage `json:"providers"`
}

// UsageStats aggregates recorded usage over the requested range. It is a pure
// read of the stats files under the user state root; it never blocks on or
// mutates any active controller. An empty stats dir yields an all-zero range.
func (a *App) UsageStats(req UsageStatsRequest) (UsageStatsRange, error) {
	from, to, err := resolveStatsRange(req)
	if err != nil {
		return UsageStatsRange{}, err
	}
	// Recording is asynchronous so chat completion never waits for disk. Give the
	// settings-only read a short read-your-own-writes window; lock contention may
	// return slightly stale statistics, but cannot stall the chat runtime.
	flushCtx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	_ = stats.Flush(flushCtx, config.StatsDir())
	cancel()
	w := stats.NewWriter(config.StatsDir())
	res, err := w.Query(stats.SourceFilter{From: from, To: to, Source: req.Source})
	if err != nil {
		return UsageStatsRange{}, err
	}
	return UsageStatsRange{
		From:        res.From,
		To:          res.To,
		Tokens:      res.Tokens,
		Requests:    res.Requests,
		Turns:       res.Turns,
		CacheHit:    res.CacheHit,
		CacheMiss:   res.CacheMiss,
		ActiveDays:  res.ActiveDays,
		TopModel:    res.TopModel,
		TopProvider: res.TopProvider,
		Daily:       res.Daily,
		Models:      res.Models,
		Providers:   res.Providers,
	}, nil
}

const (
	dateLayout              = "2006-01-02"
	maxStatsCustomRangeDays = 3660
)

// resolveStatsRange maps a request's Range into an inclusive [from, to] pair.
// "custom" requires valid From/To dates; the presets end today.
func resolveStatsRange(req UsageStatsRequest) (from, to time.Time, err error) {
	now := time.Now()
	to = time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, now.Location())
	switch req.Range {
	case "7", "14", "30", "90":
		n, err := strconv.Atoi(req.Range)
		if err != nil || n <= 0 {
			n = 7
		}
		from = to.AddDate(0, 0, -(n - 1))
		from = time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, now.Location())
	case "custom":
		f, ferr := time.ParseInLocation(dateLayout, req.From, now.Location())
		t, terr := time.ParseInLocation(dateLayout, req.To, now.Location())
		if ferr != nil || terr != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("usage stats: custom range needs valid from/to dates (2006-01-02)")
		}
		if t.Before(f) {
			return time.Time{}, time.Time{}, fmt.Errorf("usage stats: custom range from date must not be after to date")
		}
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		if t.After(today) {
			return time.Time{}, time.Time{}, fmt.Errorf("usage stats: custom range to date must not be in the future")
		}
		fUTC := time.Date(f.Year(), f.Month(), f.Day(), 0, 0, 0, 0, time.UTC)
		tUTC := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		if days := int(tUTC.Sub(fUTC)/(24*time.Hour)) + 1; days > maxStatsCustomRangeDays {
			return time.Time{}, time.Time{}, fmt.Errorf("usage stats: custom range cannot exceed %d days", maxStatsCustomRangeDays)
		}
		from = time.Date(f.Year(), f.Month(), f.Day(), 0, 0, 0, 0, now.Location())
		to = time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 0, now.Location())
	default:
		// Unknown/empty range defaults to the last 7 days.
		from = to.AddDate(0, 0, -6)
		from = time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, now.Location())
	}
	return from, to, nil
}
