package usage

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// ─── Query result types ─────────────────────────────────────────────────────

// Overview holds aggregated metrics for a time window.
type Overview struct {
	Requests         int     `json:"requests"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	CacheHitTokens   int     `json:"cache_hit_tokens"`
	CacheMissTokens  int     `json:"cache_miss_tokens"`
	ReasoningTokens  int     `json:"reasoning_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	Cost             float64 `json:"cost"`
	Currency         string  `json:"currency"`
	TPM              float64 `json:"tpm"` // tokens per minute
	RPM              float64 `json:"rpm"` // requests per minute
	FirstTS          string  `json:"first_ts,omitempty"`
	LastTS           string  `json:"last_ts,omitempty"`
}

// CacheHitRate returns the cache hit ratio (0–1), or 0 if no tokens.
func (o Overview) CacheHitRate() float64 {
	total := o.CacheHitTokens + o.CacheMissTokens
	if total == 0 {
		return 0
	}
	return float64(o.CacheHitTokens) / float64(total)
}

// ModelRow holds per-model aggregated metrics.
type ModelRow struct {
	Provider         string  `json:"provider"`
	Model            string  `json:"model"`
	Requests         int     `json:"requests"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	CacheHitTokens   int     `json:"cache_hit_tokens"`
	CacheMissTokens  int     `json:"cache_miss_tokens"`
	ReasoningTokens  int     `json:"reasoning_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	Cost             float64 `json:"cost"`
	Currency         string  `json:"currency"`
	AvgLatencyMS     float64 `json:"avg_latency_ms"`
}

// TrendPoint holds one day's aggregated metrics.
type TrendPoint struct {
	Date             string  `json:"date"`
	Requests         int     `json:"requests"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	CacheHitTokens   int     `json:"cache_hit_tokens"`
	CacheMissTokens  int     `json:"cache_miss_tokens"`
	ReasoningTokens  int     `json:"reasoning_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	Cost             float64 `json:"cost"`
	Currency         string  `json:"currency"`
}

// LogEntry is a single usage record for the tail view.
type LogEntry = Record

// ─── Query functions ────────────────────────────────────────────────────────

// QueryOverview returns aggregated metrics for the last N days.
// days <= 0 means today only.
func QueryOverview(dir string, days int) (Overview, error) {
	records, err := loadDays(dir, days)
	if err != nil {
		return Overview{}, err
	}
	var o Overview
	var firstTS, lastTS time.Time
	for _, r := range records {
		o.Requests++
		o.PromptTokens += r.PromptTokens
		o.CompletionTokens += r.CompletionTokens
		o.CacheHitTokens += r.CacheHitTokens
		o.CacheMissTokens += r.CacheMissTokens
		o.ReasoningTokens += r.ReasoningTokens
		o.TotalTokens += r.TotalTokens
		o.Cost += r.Cost
		if r.Currency != "" {
			o.Currency = r.Currency
		}
		if firstTS.IsZero() || r.TS.Before(firstTS) {
			firstTS = r.TS
		}
		if r.TS.After(lastTS) {
			lastTS = r.TS
		}
	}
	// Compute TPM/RPM based on actual data time span.
	// Minimum 1 minute to avoid division by zero / inflated rates.
	if o.Requests > 0 && !firstTS.IsZero() {
		o.FirstTS = firstTS.Format(time.RFC3339)
		o.LastTS = lastTS.Format(time.RFC3339)
		minutes := lastTS.Sub(firstTS).Minutes()
		if minutes < 1 {
			minutes = 1
		}
		o.TPM = float64(o.TotalTokens) / minutes
		o.RPM = float64(o.Requests) / minutes
	}
	return o, nil
}

// QueryModels returns per-model aggregated metrics for the last N days.
func QueryModels(dir string, days int) ([]ModelRow, error) {
	records, err := loadDays(dir, days)
	if err != nil {
		return nil, err
	}
	type key struct{ provider, model string }
	groups := map[key]*ModelRow{}
	order := []key{}
	for _, r := range records {
		k := key{r.Provider, r.Model}
		m, ok := groups[k]
		if !ok {
			m = &ModelRow{Provider: r.Provider, Model: r.Model}
			groups[k] = m
			order = append(order, k)
		}
		m.Requests++
		m.PromptTokens += r.PromptTokens
		m.CompletionTokens += r.CompletionTokens
		m.CacheHitTokens += r.CacheHitTokens
		m.CacheMissTokens += r.CacheMissTokens
		m.ReasoningTokens += r.ReasoningTokens
		m.TotalTokens += r.TotalTokens
		m.Cost += r.Cost
		if r.Currency != "" {
			m.Currency = r.Currency
		}
		m.AvgLatencyMS += float64(r.LatencyMS)
	}
	result := make([]ModelRow, 0, len(order))
	for _, k := range order {
		m := groups[k]
		if m.Requests > 0 {
			m.AvgLatencyMS /= float64(m.Requests)
		}
		result = append(result, *m)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Cost > result[j].Cost })
	return result, nil
}

// QueryTrend returns daily aggregated metrics for the last N days.
func QueryTrend(dir string, days int) ([]TrendPoint, error) {
	records, err := loadDays(dir, days)
	if err != nil {
		return nil, err
	}
	daysMap := map[string]*TrendPoint{}
	order := []string{}
	for _, r := range records {
		d := r.TS.Format("2006-01-02")
		t, ok := daysMap[d]
		if !ok {
			t = &TrendPoint{Date: d}
			daysMap[d] = t
			order = append(order, d)
		}
		t.Requests++
		t.PromptTokens += r.PromptTokens
		t.CompletionTokens += r.CompletionTokens
		t.CacheHitTokens += r.CacheHitTokens
		t.CacheMissTokens += r.CacheMissTokens
		t.ReasoningTokens += r.ReasoningTokens
		t.TotalTokens += r.TotalTokens
		t.Cost += r.Cost
		if r.Currency != "" {
			t.Currency = r.Currency
		}
	}
	sort.Strings(order)
	result := make([]TrendPoint, 0, len(order))
	for _, d := range order {
		result = append(result, *daysMap[d])
	}
	return result, nil
}

// QueryLogs returns the most recent N usage records, optionally filtered by
// provider and/or model. If days > 0, only records from the last N days are considered.
func QueryLogs(dir string, limit int, provider, model string, days int) ([]LogEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	// Scan all available files (we need newest-first, so reverse the order).
	files, err := listJSONLFiles(dir)
	if err != nil {
		return nil, err
	}
	// Filter by days if requested
	if days > 0 {
		since := time.Now().AddDate(0, 0, -days).Format("2006-01-02")
		filtered := make([]string, 0, len(files))
		for _, f := range files {
			day := f[:len(f)-6] // strip ".jsonl"
			if day >= since {
				filtered = append(filtered, f)
			}
		}
		files = filtered
	}
	// Reverse to get newest day first.
	for i, j := 0, len(files)-1; i < j; i, j = i+1, j-1 {
		files[i], files[j] = files[j], files[i]
	}
	var result []LogEntry
	for _, f := range files {
		if len(result) >= limit {
			break
		}
		records, err := readFile(filepath.Join(dir, f))
		if err != nil {
			continue
		}
		// Append in reverse (newest within the day first).
		for i := len(records) - 1; i >= 0 && len(result) < limit; i-- {
			r := records[i]
			if provider != "" && r.Provider != provider {
				continue
			}
			if model != "" && r.Model != model {
				continue
			}
			result = append(result, r)
		}
	}
	return result, nil
}

// QueryProviders returns the distinct provider names found in the store.
func QueryProviders(dir string) ([]string, error) {
	records, err := loadAll(dir)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var result []string
	for _, r := range records {
		if r.Provider != "" && !seen[r.Provider] {
			seen[r.Provider] = true
			result = append(result, r.Provider)
		}
	}
	sort.Strings(result)
	return result, nil
}

// QueryModelNames returns the distinct model names found in the store.
func QueryModelNames(dir string) ([]string, error) {
	records, err := loadAll(dir)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var result []string
	for _, r := range records {
		if r.Model != "" && !seen[r.Model] {
			seen[r.Model] = true
			result = append(result, r.Model)
		}
	}
	sort.Strings(result)
	return result, nil
}

// ─── Internal helpers ───────────────────────────────────────────────────────

// loadDays loads all records from the last N days. days <= 0 means today only.
// loadAll loads all records from all JSONL files in the directory.
func loadAll(dir string) ([]Record, error) {
	files, err := listJSONLFiles(dir)
	if err != nil {
		return nil, err
	}
	var all []Record
	for _, name := range files {
		records, err := readFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		all = append(all, records...)
	}
	return all, nil
}

func loadDays(dir string, days int) ([]Record, error) {
	files, err := listJSONLFiles(dir)
	if err != nil {
		return nil, err
	}
	since := time.Now().AddDate(0, 0, -days).Format("2006-01-02")
	var all []Record
	for _, name := range files {
		day := name[:len(name)-6] // strip ".jsonl"
		if day < since {
			continue
		}
		records, err := readFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		all = append(all, records...)
	}
	return all, nil
}

// readFile reads all records from a single JSONL file.
func readFile(path string) ([]Record, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var records []Record
	scanner := bufio.NewScanner(f)
	// Allow up to 4 KB per line (generous for ~200-byte records).
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)
	for scanner.Scan() {
		var r Record
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			continue // skip malformed lines
		}
		records = append(records, r)
	}
	return records, scanner.Err()
}

// listJSONLFiles returns YYYY-MM-DD.jsonl filenames in sorted order.
func listJSONLFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if len(name) == 16 && name[10] == '.' && name[11:] == "jsonl" && name[4] == '-' && name[7] == '-' {
			names = append(names, name)
		}
	}
	sort.Strings(names) // chronological
	return names, nil
}
