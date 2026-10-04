package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/state/stats"
)

// A historical month or arbitrary range is a query, not a fixed window: the
// panel must be able to ask for dates that are no longer inside its 30-day
// default.
func TestUsageAcceptsAnInclusiveDateRange(t *testing.T) {
	s := newListenerTestServer(t)
	day := time.Now().AddDate(0, -2, 0)
	line := map[string]any{"ts": day.Format(time.RFC3339), "model": "deepseek-flash/deepseek-flash", "source": "desktop",
		"total": 100, "cost_amount": "0.5", "cost_currency": "USD"}
	encoded, _ := json.Marshal(line)
	if err := os.MkdirAll(config.StatsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config.StatsDir(), day.Format("2006-01-02")+".jsonl"), append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	url := "/usage?from=" + day.Format("2006-01-02") + "&to=" + day.Format("2006-01-02")
	s.usage(rec, httptest.NewRequest(http.MethodGet, url, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got stats.RangeStats
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.From != day.Format("2006-01-02") || got.To != day.Format("2006-01-02") || got.Tokens != 100 {
		t.Fatalf("range = %s..%s tokens=%d, want that exact historical day", got.From, got.To, got.Tokens)
	}
}

// The usage panel reads money in the currency the session's own costs read in,
// so a CNY wallet sees a CNY total wherever the vendor published a CNY price.
func TestUsageRejectsInvalidExplicitRanges(t *testing.T) {
	s := newListenerTestServer(t)
	for _, target := range []string{
		"/usage?from=2026-01-02&to=2026-01-01",
		"/usage?from=2026-01-01",
		"/usage?from=not-a-date&to=2026-01-01",
		"/usage?from=2025-01-01&to=2026-01-01",
	} {
		t.Run(target, func(t *testing.T) {
			rec := httptest.NewRecorder()
			s.usage(rec, httptest.NewRequest(http.MethodGet, target, nil))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestUsageReadsInTheSessionDisplayCurrency(t *testing.T) {
	s := newListenerTestServer(t)
	now := time.Now()
	line := map[string]any{"ts": now.Format(time.RFC3339), "model": "deepseek-flash/deepseek-flash", "source": "desktop",
		"total": 100, "cost_amount": "0.5", "cost_currency": "USD", "valuation_usd": "0.5", "valuation_cny": "3.5"}
	encoded, _ := json.Marshal(line)
	if err := os.MkdirAll(config.StatsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config.StatsDir(), now.Format("2006-01-02")+".jsonl"), append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	read := func() stats.RangeStats {
		rec := httptest.NewRecorder()
		s.usage(rec, httptest.NewRequest(http.MethodGet, "/usage?days=1", nil))
		var got stats.RangeStats
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("status %d: %v", rec.Code, err)
		}
		return got
	}
	if got := read().Cost; len(got) != 1 || got[0].Currency != "USD" || got[0].Amount != "0.5" {
		t.Fatalf("no display currency = %+v, want the billed USD", got)
	}
	s.bc.SetDisplayCurrency("CNY")
	if got := read().Cost; len(got) != 1 || got[0].Currency != "CNY" || got[0].Amount != "3.5" {
		t.Fatalf("CNY display = %+v, want the vendor's CNY price", got)
	}
}
