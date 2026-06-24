package usage

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// NewDashboardHandler returns an http.Handler that serves a usage dashboard
// and its JSON API. dir is the JSONL store directory.
func NewDashboardHandler(dir string, defaultDays int) http.Handler {
	mux := http.NewServeMux()
	s := &dashboardServer{dir: dir, defaultDays: defaultDays}

	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/overview", s.handleOverview)
	mux.HandleFunc("/api/trend", s.handleTrend)
	mux.HandleFunc("/api/models", s.handleModels)
	mux.HandleFunc("/api/logs", s.handleLogs)
	mux.HandleFunc("/api/export", s.handleExport)

	return mux
}

type dashboardServer struct {
	dir         string
	defaultDays int
}

func (s *dashboardServer) daysParam(r *http.Request) int {
	if v := r.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return s.defaultDays
}

func (s *dashboardServer) handleOverview(w http.ResponseWriter, r *http.Request) {
	days := s.daysParam(r)
	overview, err := QueryOverview(s.dir, days)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	models, _ := QueryModels(s.dir, days)
	writeJSON(w, map[string]any{"overview": overview, "models": models})
}

func (s *dashboardServer) handleTrend(w http.ResponseWriter, r *http.Request) {
	days := s.daysParam(r)
	data, err := QueryTrend(s.dir, days)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, data)
}

func (s *dashboardServer) handleModels(w http.ResponseWriter, r *http.Request) {
	days := s.daysParam(r)
	data, err := QueryModels(s.dir, days)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, data)
}

func (s *dashboardServer) handleLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 50
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	days := s.daysParam(r)
	data, err := QueryLogs(s.dir, limit, q.Get("provider"), q.Get("model"), days)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, data)
}

func (s *dashboardServer) handleExport(w http.ResponseWriter, r *http.Request) {
	days := s.daysParam(r)
	q := r.URL.Query()
	format := q.Get("format")
	if format == "" {
		format = "csv"
	}

	entries, err := QueryLogs(s.dir, 10000, "", "", 0)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	// Filter by days
	cutoff := time.Now().AddDate(0, 0, -days).Format("2006-01-02")
	var filtered []LogEntry
	for _, e := range entries {
		if e.TS.Format("2006-01-02") >= cutoff {
			filtered = append(filtered, e)
		}
	}

	switch format {
	case "csv":
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", "attachment; filename=usage.csv")
		cw := csv.NewWriter(w)
		cw.Write([]string{"ts", "provider", "model", "usage_source", "prompt_tokens", "completion_tokens", "cache_hit_tokens", "cache_miss_tokens", "reasoning_tokens", "total_tokens", "cost", "currency", "latency_ms"})
		for _, e := range filtered {
			cw.Write([]string{
				e.TS.Format(time.RFC3339), e.Provider, e.Model, e.UsageSource,
				strconv.Itoa(e.PromptTokens), strconv.Itoa(e.CompletionTokens),
				strconv.Itoa(e.CacheHitTokens), strconv.Itoa(e.CacheMissTokens),
				strconv.Itoa(e.ReasoningTokens), strconv.Itoa(e.TotalTokens),
				fmt.Sprintf("%.6f", e.Cost), e.Currency,
				strconv.FormatInt(e.LatencyMS, 10),
			})
		}
		cw.Flush()
	default:
		writeJSON(w, filtered)
	}
}

func (s *dashboardServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, dashboardHTML)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}
