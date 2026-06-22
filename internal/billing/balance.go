// Package billing queries a provider's wallet balance for the status line. The
// only documented shape today is DeepSeek's GET /user/balance. Balance is
// strictly optional: a provider with no balance_url is never queried — callers
// pass "" and get (nil, nil) back, and surfaces simply omit the readout.
//
// For providers with custom balance APIs, use NewFetch with BalanceEntry to
// configure method, body, and JSON response path. Kept tiny and dependency-free
// (net/http + encoding/json) so every frontend can share one fetch.
package billing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// BalanceEntry configures a provider-specific wallet balance query.
// Empty Method/ResponsePath falls back to the legacy DeepSeek GET /user/balance.
type BalanceEntry struct {
	URL          string // e.g. "https://api.deepseek.com/user/balance"
	Method       string // HTTP method; "GET" (default) or "POST"
	Body         string // optional JSON body for POST requests
	ResponsePath string // JSONPath expression to extract balance value, e.g. "$.balance_infos[0].total_balance"
	Currency     string // Currency label for display; "CNY" (default), "USD", etc.
}

// Balance is a wallet balance normalized for display.
type Balance struct {
	Available bool   // the provider reports the account can still serve API calls
	Infos     []Info // one entry per currency the provider returns
}

// Info is one currency's balance (DeepSeek returns one per currency).
type Info struct {
	Currency        string // "CNY" | "USD"
	TotalBalance    string // total available (granted + topped-up)
	GrantedBalance  string // unexpired promotional credit
	ToppedUpBalance string // paid-in credit
}

// deepseekResp mirrors the GET /user/balance response shape.
type deepseekResp struct {
	IsAvailable  bool `json:"is_available"`
	BalanceInfos []struct {
		Currency        string `json:"currency"`
		TotalBalance    string `json:"total_balance"`
		GrantedBalance  string `json:"granted_balance"`
		ToppedUpBalance string `json:"topped_up_balance"`
	} `json:"balance_infos"`
}

// httpClient bounds the balance query so a slow endpoint can't hang the status
// line; the per-call ctx still cancels it on shutdown.
var httpClient = &http.Client{Timeout: 12 * time.Second}

// Fetch queries a DeepSeek-style request with url + Bearer apiKey and returns
// the normalized balance. An empty url yields (nil, nil) — "not configured",
// not an error — so callers can treat both the same and just omit the readout.
func Fetch(ctx context.Context, url, apiKey string) (*Balance, error) {
	return FetchWithClient(ctx, httpClient, url, apiKey)
}

// NewFetch queries a provider's wallet balance with a custom BalanceEntry.
// Supports configurable HTTP method, body, and JSON response path extraction.
// If entry has no ResponsePath, falls back to DeepSeek-style parsing.
func NewFetch(ctx context.Context, apiKey string, entry BalanceEntry) (*Balance, error) {
	return newFetchWithClient(ctx, httpClient, apiKey, entry)
}

// FetchWithClient queries the balance endpoint using the caller-provided client.
// A nil client falls back to the package default.
func FetchWithClient(ctx context.Context, client *http.Client, url, apiKey string) (*Balance, error) {
	if strings.TrimSpace(url) == "" {
		return nil, nil
	}
	if client == nil {
		client = httpClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("balance: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var dr deepseekResp
	if err := json.Unmarshal(body, &dr); err != nil {
		return nil, fmt.Errorf("balance: decode: %w", err)
	}
	b := &Balance{Available: dr.IsAvailable}
	for _, bi := range dr.BalanceInfos {
		b.Infos = append(b.Infos, Info{
			Currency:        bi.Currency,
			TotalBalance:    bi.TotalBalance,
			GrantedBalance:  bi.GrantedBalance,
			ToppedUpBalance: bi.ToppedUpBalance,
		})
	}
	return b, nil
}

// newFetchWithClient queries the balance endpoint using a custom BalanceEntry.
func newFetchWithClient(ctx context.Context, client *http.Client, apiKey string, entry BalanceEntry) (*Balance, error) {
	url := strings.TrimSpace(entry.URL)
	if url == "" {
		return nil, nil
	}
	if client == nil {
		client = httpClient
	}
	method := entry.Method
	if method == "" {
		method = http.MethodGet
	}
	var bodyReader io.Reader
	if entry.Body != "" {
		bodyReader = bytes.NewReader([]byte(entry.Body))
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("balance: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("balance: request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("balance: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	// If no custom response path, parse as DeepSeek format.
	if entry.ResponsePath == "" {
		return decodeDeepSeek(body)
	}
	// Extract balance value via simple dot-notation path.
	val, err := extractJSONPath(body, entry.ResponsePath)
	if err != nil {
		return nil, fmt.Errorf("balance: extract: %w", err)
	}
	currency := entry.Currency
	if currency == "" {
		currency = "CNY"
	}
	b := &Balance{Available: true}
	b.Infos = append(b.Infos, Info{
		Currency:     currency,
		TotalBalance: val,
	})
	return b, nil
}

func decodeDeepSeek(body []byte) (*Balance, error) {
	var dr deepseekResp
	if err := json.Unmarshal(body, &dr); err != nil {
		return nil, fmt.Errorf("balance: decode: %w", err)
	}
	b := &Balance{Available: dr.IsAvailable}
	for _, bi := range dr.BalanceInfos {
		b.Infos = append(b.Infos, Info{
			Currency:        bi.Currency,
			TotalBalance:    bi.TotalBalance,
			GrantedBalance:  bi.GrantedBalance,
			ToppedUpBalance: bi.ToppedUpBalance,
		})
	}
	return b, nil
}

// extractJSONPath walks a simple dot-delimited JSON path (e.g. "$.data.balance"
// or "$.balance_infos[0].total_balance") and returns the matched value as a
// string. Supports array index with [n] syntax.
func extractJSONPath(body []byte, path string) (string, error) {
	if !strings.HasPrefix(path, "$") {
		return "", fmt.Errorf("path must start with $")
	}
	segments := strings.Split(strings.TrimPrefix(path, "$."), ".")
	var raw any
	if err := json.Unmarshal(body, &raw); err != nil {
		return "", fmt.Errorf("parse JSON: %w", err)
	}
	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		// Check for array index: field[0]
		if idx := strings.Index(seg, "["); idx >= 0 {
			field := seg[:idx]
			rest := seg[idx:]
			if field != "" {
				m, ok := raw.(map[string]any)
				if !ok {
					return "", fmt.Errorf("expected object at %q", field)
				}
				raw = m[field]
				if raw == nil {
					return "", fmt.Errorf("field %q not found", field)
				}
			}
			// Parse [n]
			if rest[0] == '[' && rest[len(rest)-1] == ']' {
				var idx int
				if _, err := fmt.Sscanf(rest, "[%d]", &idx); err != nil {
					return "", fmt.Errorf("invalid array index %q: %w", rest, err)
				}
				arr, ok := raw.([]any)
				if !ok {
					return "", fmt.Errorf("expected array at %q", rest)
				}
				if idx < 0 || idx >= len(arr) {
					return "", fmt.Errorf("index %d out of range (len=%d)", idx, len(arr))
				}
				raw = arr[idx]
			}
		} else {
			m, ok := raw.(map[string]any)
			if !ok {
				return "", fmt.Errorf("expected object at %q", seg)
			}
			raw = m[seg]
			if raw == nil {
				return "", fmt.Errorf("field %q not found", seg)
			}
		}
	}
	// Format the result as a string.
	switch v := raw.(type) {
	case float64:
		return fmt.Sprintf("%.2f", v), nil
	case string:
		return v, nil
	default:
		b, _ := json.Marshal(v)
		return string(b), nil
	}
}

// symbol maps an ISO currency code to a compact symbol; an unknown code passes
// through with a trailing space ("XYZ 12.00").
func symbol(currency string) string {
	switch strings.ToUpper(currency) {
	case "CNY", "RMB":
		return "\u00a5"
	case "USD":
		return "$"
	default:
		if currency == "" {
			return ""
		}
		return currency + " "
	}
}

// Display renders the primary balance compactly, e.g. "\u00a5110.00". It prefers CNY,
// then the first currency reported. "" when there's nothing to show.
func (b *Balance) Display() string {
	if b == nil || len(b.Infos) == 0 {
		return ""
	}
	pick := b.Infos[0]
	for _, i := range b.Infos {
		if strings.EqualFold(i.Currency, "CNY") {
			pick = i
			break
		}
	}
	return symbol(pick.Currency) + strings.TrimSpace(pick.TotalBalance)
}
