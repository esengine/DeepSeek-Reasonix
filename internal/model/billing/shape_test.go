package billing

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

const moonshotWallet = `{"code":0,"data":{"available_balance":49.58,"voucher_balance":9.58,"cash_balance":40.00},"scode":"0x0","status":true}`

// A vendor whose wallet answers in its own shape is read in that shape. Before
// the table, every endpoint was read as DeepSeek's and this one decoded into an
// empty balance that rendered as an unusable account.
func TestMoonshotWalletDecodes(t *testing.T) {
	b, err := decodeWallet("https://api.moonshot.cn/v1/users/me/balance", []byte(moonshotWallet))
	if err != nil {
		t.Fatal(err)
	}
	if !b.Available {
		t.Error("a funded wallet read back as unusable")
	}
	if got := b.Display(); got != "¥49.58" {
		t.Errorf("Display = %q, want ¥49.58", got)
	}
	if got := b.Infos[0].GrantedBalance; got != "9.58" {
		t.Errorf("granted = %q, want 9.58", got)
	}
	if got := b.Infos[0].ToppedUpBalance; got != "40.00" {
		t.Errorf("topped up = %q, want 40.00", got)
	}
}

// The currency is not in the body: the two endpoints are the two accounts.
func TestMoonshotCurrencyFollowsEndpoint(t *testing.T) {
	b, err := decodeWallet("https://api.moonshot.ai/v1/users/me/balance", []byte(moonshotWallet))
	if err != nil {
		t.Fatal(err)
	}
	if got := b.PrimaryCurrency(); got != "USD" {
		t.Errorf("PrimaryCurrency = %q, want USD", got)
	}
}

// The regression this table exists for: a response in another shape parses as
// JSON without erroring, and reporting that as a zero balance states a fact
// nobody sent.
func TestForeignShapeIsNotAnEmptyWallet(t *testing.T) {
	_, err := decodeWallet("https://relay.example.com/balance", []byte(moonshotWallet))
	if !errors.Is(err, ErrUnreadable) {
		t.Fatalf("err = %v, want ErrUnreadable", err)
	}
}

// A wallet that will not answer and a key that was refused are different things
// to do next, so they arrive as different identities rather than one sentence.
func TestFetchStatusCarriesIdentity(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   error
	}{
		{http.StatusUnauthorized, ErrUnauthorized},
		{http.StatusForbidden, ErrUnauthorized},
		{http.StatusInternalServerError, ErrUnreachable},
		{http.StatusNotFound, ErrUnreachable},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = io.WriteString(w, "nope")
		}))
		_, err := Fetch(context.Background(), srv.URL, "key")
		srv.Close()
		if !errors.Is(err, tc.want) {
			t.Errorf("status %d gave %v, want %v", tc.status, err, tc.want)
		}
	}
}

// OpenRouter's credits endpoint reports what was bought and what was spent;
// the wallet is the difference, and it is always USD.
func TestOpenRouterCreditsDecode(t *testing.T) {
	b, err := decodeWallet("https://openrouter.ai/api/v1/credits", []byte(`{"data":{"total_credits":10,"total_usage":0.25}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !b.Available {
		t.Error("a funded wallet read back as unusable")
	}
	if got := b.Display(); got != "$9.75" {
		t.Errorf("Display = %q, want $9.75", got)
	}
	if got := b.PrimaryCurrency(); got != "USD" {
		t.Errorf("PrimaryCurrency = %q, want USD", got)
	}
}

func TestOpenRouterSpentWalletIsUnavailable(t *testing.T) {
	b, err := decodeWallet("https://openrouter.ai/api/v1/credits", []byte(`{"data":{"total_credits":5,"total_usage":5}}`))
	if err != nil {
		t.Fatal(err)
	}
	if b.Available {
		t.Error("a spent wallet read back as usable")
	}
}

func TestOpenRouterWithoutDataIsUnreadable(t *testing.T) {
	_, err := decodeWallet("https://openrouter.ai/api/v1/credits", []byte(`{"error":{"code":401}}`))
	if !errors.Is(err, ErrUnreadable) {
		t.Fatalf("err = %v, want ErrUnreadable", err)
	}
}
