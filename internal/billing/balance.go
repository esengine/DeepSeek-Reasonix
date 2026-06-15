// Package billing queries a provider's wallet balance for the status line. The
// only documented shape today is DeepSeek's GET /user/balance, so Fetch speaks
// that schema. Balance is strictly optional: a provider with no balance_url is
// never queried — callers pass "" and get (nil, nil) back, and surfaces simply
// omit the readout. Kept tiny and dependency-free (net/http + encoding/json) so
// every frontend can share one fetch.
package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

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

// Fetch queries url (a DeepSeek-style balance endpoint) with a Bearer apiKey and
// returns the normalized balance. An empty url yields (nil, nil) — "not
// configured", not an error — so callers can treat both the same and just omit
// the readout.
func Fetch(ctx context.Context, url, apiKey string) (*Balance, error) {
	return FetchWithClient(ctx, httpClient, url, apiKey)
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

// symbol maps an ISO currency code to a compact symbol; an unknown code passes
// through with a trailing space ("XYZ 12.00").
func symbol(currency string) string {
	switch strings.ToUpper(currency) {
	case "CNY", "RMB":
		return "¥"
	case "USD":
		return "$"
	default:
		if currency == "" {
			return ""
		}
		return currency + " "
	}
}

// Display renders the primary balance compactly, e.g. "¥110.00". It prefers CNY,
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
