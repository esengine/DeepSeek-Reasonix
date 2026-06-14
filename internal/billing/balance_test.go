package billing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A DeepSeek-shaped response parses, exposes Available, and Display prefers CNY
// with the right symbol; the request carries the bearer key.
func TestFetchDeepSeekShape(t *testing.T) {
	const body = `{
		"is_available": true,
		"balance_infos": [
			{"currency": "USD", "total_balance": "15.30", "granted_balance": "0.00", "topped_up_balance": "15.30"},
			{"currency": "CNY", "total_balance": "110.00", "granted_balance": "10.00", "topped_up_balance": "100.00"}
		]
	}`
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	b, err := Fetch(context.Background(), srv.URL, "secret-key")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if b == nil || !b.Available {
		t.Fatalf("want available balance, got %+v", b)
	}
	if gotAuth != "Bearer secret-key" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer secret-key")
	}
	if len(b.Infos) != 2 {
		t.Fatalf("want 2 infos, got %d", len(b.Infos))
	}
	// Display prefers CNY → "¥110.00", not the first (USD) entry.
	if got := b.Display(); got != "¥110.00" {
		t.Errorf("Display = %q, want %q", got, "¥110.00")
	}
}

// An empty url is "not configured", not an error: (nil, nil), and Display on a nil
// balance is "".
func TestFetchEmptyURL(t *testing.T) {
	b, err := Fetch(context.Background(), "", "key")
	if err != nil || b != nil {
		t.Fatalf("Fetch(\"\") = (%v, %v), want (nil, nil)", b, err)
	}
	if got := b.Display(); got != "" {
		t.Errorf("nil Display = %q, want empty", got)
	}
}

// A non-200 surfaces an error rather than a bogus zero balance.
func TestFetchHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid key"}`))
	}))
	defer srv.Close()
	if _, err := Fetch(context.Background(), srv.URL, "bad"); err == nil {
		t.Fatal("want error on 401, got nil")
	}
}

// Display falls back to the first currency when no CNY entry is present, and maps
// USD to "$".
func TestDisplayUSDOnly(t *testing.T) {
	b := &Balance{Available: true, Infos: []Info{{Currency: "USD", TotalBalance: "9.99"}}}
	if got := b.Display(); got != "$9.99" {
		t.Errorf("Display = %q, want %q", got, "$9.99")
	}
}

// DisplayAll joins all currencies with separator.
func TestDisplayAllDefault(t *testing.T) {
	b := &Balance{Available: true, Infos: []Info{
		{Currency: "USD", TotalBalance: "15.30"},
		{Currency: "CNY", TotalBalance: "110.00"},
	}}
	if got := b.DisplayAll(" | "); got != "$15.30 | ¥110.00" {
		t.Errorf("DisplayAll = %q, want %q", got, "$15.30 | ¥110.00")
	}
}

// DisplayAll returns "" on nil balance, same as Display.
func TestDisplayAllNil(t *testing.T) {
	var b *Balance
	if got := b.DisplayAll(" | "); got != "" {
		t.Errorf("nil DisplayAll = %q, want empty", got)
	}
}

// DisplayAll returns "" on empty Infos.
func TestDisplayAllEmptyInfos(t *testing.T) {
	b := &Balance{Available: true}
	if got := b.DisplayAll(" | "); got != "" {
		t.Errorf("empty infos DisplayAll = %q, want empty", got)
	}
}

// DisplayAll handles single currency without separator.
func TestDisplayAllSingle(t *testing.T) {
	b := &Balance{Infos: []Info{
		{Currency: "USD", TotalBalance: "9.99"},
	}}
	if got := b.DisplayAll(" | "); got != "$9.99" {
		t.Errorf("DisplayAll = %q, want %q", got, "$9.99")
	}
}

// DisplayAll handles three currencies.
func TestDisplayAllThreeCurrencies(t *testing.T) {
	b := &Balance{Infos: []Info{
		{Currency: "USD", TotalBalance: "10.00"},
		{Currency: "CNY", TotalBalance: "50.00"},
		{Currency: "EUR", TotalBalance: "20.00"},
	}}
	if got := b.DisplayAll(" | "); got != "$10.00 | ¥50.00 | EUR 20.00" {
		t.Errorf("DisplayAll = %q, want %q", got, "$10.00 | ¥50.00 | EUR 20.00")
	}
}

// DisplayAll uses custom separator.
func TestDisplayAllCustomSep(t *testing.T) {
	b := &Balance{Infos: []Info{
		{Currency: "USD", TotalBalance: "5.00"},
		{Currency: "CNY", TotalBalance: "30.00"},
	}}
	if got := b.DisplayAll("  "); got != "$5.00  ¥30.00" {
		t.Errorf("DisplayAll = %q, want %q", got, "$5.00  ¥30.00")
	}
}
