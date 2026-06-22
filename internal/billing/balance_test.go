package billing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExtractJSONPathSimple(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		path    string
		want    string
		wantErr bool
	}{
		{name: "top-level field", json: `{"balance": 100.50}`, path: "$.balance", want: "100.50"},
		{name: "nested field", json: `{"data": {"balance": 50}}`, path: "$.data.balance", want: "50.00"},
		{name: "array index", json: `{"infos": [{"total": "99.99"}]}`, path: "$.infos[0].total", want: "99.99"},
		{name: "missing field", json: `{"x": 1}`, path: "$.y", wantErr: true},
		{name: "invalid path prefix", json: `{}`, path: "data.balance", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractJSONPath([]byte(tt.json), tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractJSONPath() error = %v, wantErr = %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("extractJSONPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSymbolCNY(t *testing.T) {
	s := symbol("CNY")
	if s != "\u00a5" {
		t.Fatalf("CNY symbol: got %q, want ¥", s)
	}
}

func TestSymbolRMB(t *testing.T) {
	s := symbol("RMB")
	if s != "\u00a5" {
		t.Fatalf("RMB symbol: got %q, want ¥", s)
	}
}

func TestSymbolUSD(t *testing.T) {
	s := symbol("USD")
	if s != "$" {
		t.Fatalf("USD symbol: got %q, want $", s)
	}
}

func TestSymbolUnknown(t *testing.T) {
	s := symbol("EUR")
	if s != "EUR " {
		t.Fatalf("EUR symbol: got %q, want 'EUR '", s)
	}
}

func TestSymbolEmpty(t *testing.T) {
	s := symbol("")
	if s != "" {
		t.Fatalf("empty currency symbol: got %q, want ''", s)
	}
}

func TestSymbolLowercase(t *testing.T) {
	s := symbol("cny")
	if s != "\u00a5" {
		t.Fatalf("lowercase cny symbol: got %q, want ¥", s)
	}
}

func TestDisplayNil(t *testing.T) {
	b := (*Balance)(nil)
	d := b.Display()
	if d != "" {
		t.Fatalf("nil display: got %q, want ''", d)
	}
}

func TestDisplayEmptyInfos(t *testing.T) {
	b := &Balance{Infos: []Info{}}
	d := b.Display()
	if d != "" {
		t.Fatalf("empty infos display: got %q, want ''", d)
	}
}

func TestDisplayPrefersCNY(t *testing.T) {
	b := &Balance{Infos: []Info{
		{Currency: "USD", TotalBalance: "100.00"},
		{Currency: "CNY", TotalBalance: "200.00"},
	}}
	d := b.Display()
	if d != "\u00a5200.00" {
		t.Fatalf("prefers CNY: got %q, want ¥200.00", d)
	}
}

func TestDisplayFallsBackToFirst(t *testing.T) {
	b := &Balance{Infos: []Info{
		{Currency: "USD", TotalBalance: "99.99"},
	}}
	d := b.Display()
	if d != "$99.99" {
		t.Fatalf("first currency: got %q, want $99.99", d)
	}
}

func TestDisplayUSDOnly(t *testing.T) {
	b := &Balance{Infos: []Info{
		{Currency: "USD", TotalBalance: "50.00"},
	}}
	d := b.Display()
	if d != "$50.00" {
		t.Fatalf("USD only: got %q, want $50.00", d)
	}
}

func TestFetchContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"100.00"}]}`))
	}))
	defer srv.Close()
	_, err := Fetch(ctx, srv.URL, "test-key")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestFetchMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{bad json`))
	}))
	defer srv.Close()
	_, err := Fetch(context.Background(), srv.URL, "test-key")
	if err == nil {
		t.Fatal("expected error for malformed json")
	}
}

func TestFetchNoAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("expected no Authorization header")
		}
		w.Write([]byte(`{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"100.00"}]}`))
	}))
	defer srv.Close()
	b, err := Fetch(context.Background(), srv.URL, "")
	if err != nil {
		t.Fatalf("fetch without api key: %v", err)
	}
	if !b.Available {
		t.Fatal("expected available true")
	}
}

func TestFetchWhitespaceURL(t *testing.T) {
	b, err := Fetch(context.Background(), "  ", "key")
	if err != nil {
		t.Fatalf("whitespace url should not error: %v", err)
	}
	if b != nil {
		t.Fatal("whitespace url should return nil balance")
	}
}

func TestFetchServerUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	_, err := Fetch(context.Background(), srv.URL, "key")
	if err == nil {
		t.Fatal("expected error for 503")
	}
}

func TestFetchHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`rate limited`))
	}))
	defer srv.Close()
	_, err := Fetch(context.Background(), srv.URL, "key")
	if err == nil {
		t.Fatal("expected error for 403")
	}
}

func TestFetchEmptyURL(t *testing.T) {
	b, err := Fetch(context.Background(), "", "key")
	if err != nil {
		t.Fatalf("empty url should not error: %v", err)
	}
	if b != nil {
		t.Fatal("empty url should return nil balance")
	}
}

func TestFetchDeepSeekShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("expected Bearer auth")
		}
		w.Write([]byte(`{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"110.00","granted_balance":"0.00","topped_up_balance":"110.00"}]}`))
	}))
	defer srv.Close()
	b, err := FetchWithClient(context.Background(), nil, srv.URL, "test-key")
	if err != nil {
		t.Fatalf("fetch deepseek: %v", err)
	}
	if !b.Available {
		t.Fatal("expected available")
	}
	if len(b.Infos) != 1 {
		t.Fatalf("expected 1 info, got %d", len(b.Infos))
	}
	if b.Infos[0].TotalBalance != "110.00" {
		t.Fatalf("total balance: got %q, want 110.00", b.Infos[0].TotalBalance)
	}
	if b.Infos[0].Currency != "CNY" {
		t.Fatalf("currency: got %q, want CNY", b.Infos[0].Currency)
	}
	disp := b.Display()
	if disp != "\u00a5110.00" {
		t.Fatalf("display: got %q, want ¥110.00", disp)
	}
}

func TestNewFetchCustomPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data": {"balance": 88.50, "currency": "CNY"}}`))
	}))
	defer srv.Close()
	b, err := newFetchWithClient(context.Background(), nil, "test-key", BalanceEntry{
		URL:          srv.URL,
		ResponsePath: "$.data.balance",
		Currency:     "CNY",
	})
	if err != nil {
		t.Fatalf("NewFetch custom path: %v", err)
	}
	if !b.Available {
		t.Fatal("expected available")
	}
	if len(b.Infos) != 1 {
		t.Fatalf("expected 1 info, got %d", len(b.Infos))
	}
	if b.Infos[0].TotalBalance != "88.50" {
		t.Fatalf("balance: got %q, want 88.50", b.Infos[0].TotalBalance)
	}
	disp := b.Display()
	if disp != "\u00a588.50" {
		t.Fatalf("display: got %q, want ¥88.50", disp)
	}
}

func TestNewFetchPOST(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("X-Custom") != "test-value" {
			t.Fatalf("expected X-Custom header")
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["action"] != "query_balance" {
			t.Fatalf("expected action=query_balance, got %v", body["action"])
		}
		w.Write([]byte(`{"remaining": 200.00}`))
	}))
	defer srv.Close()
	b, err := newFetchWithClient(context.Background(), nil, "test-key", BalanceEntry{
		URL:          srv.URL,
		Method:       "POST",
		Body:         `{"action": "query_balance"}`,
		Headers:      map[string]string{"X-Custom": "test-value"},
		ResponsePath: "$.remaining",
		Currency:     "CNY",
	})
	if err != nil {
		t.Fatalf("NewFetch POST: %v", err)
	}
	if b.Infos[0].TotalBalance != "200.00" {
		t.Fatalf("balance: got %q, want 200.00", b.Infos[0].TotalBalance)
	}
}
