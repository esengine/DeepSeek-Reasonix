package serve

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
	"reasonix/internal/safety/typesafe"
)

// ollamaLike answers the way a local Ollama does: chat lives only under /v1,
// an unknown model is a 404 whose error carries no machine-readable code, and
// a foreign Origin is a 403.
func ollamaLike(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var req struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Model != "nimble:9b" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprintf(w, `{"error":{"message":"model %q not found, try pulling it first","type":"api_error","param":null,"code":null}}`, req.Model)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"O\"}}]}\n\ndata: [DONE]\n\n")
	}))
}

func TestCheckProviderModelAgainstAnOllamaShapedEndpoint(t *testing.T) {
	upstream := ollamaLike(t)
	defer upstream.Close()
	tests := []struct {
		name, base, model, kind string
		status, reason          string
		httpStatus              int
	}{
		{"correct", upstream.URL + "/v1", "nimble:9b", "openai", "available", "", 0},
		{"base without /v1", upstream.URL, "nimble:9b", "openai", "unknown", "rejected", http.StatusNotFound},
		{"model id without tag", upstream.URL + "/v1", "nimble", "openai", "unknown", "rejected", http.StatusNotFound},
		{"still System One", upstream.URL + "/v1", "nimble:9b", "typesafe", "unknown", "rejected", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newProviderEditServer(t)
			s.AllowProviderEdit()
			srv := httptest.NewServer(operatorHandler(s))
			defer srv.Close()
			body := fmt.Sprintf(`{"name":"ollama","model":%q,"baseUrl":%q,"kind":%q}`, tt.model, tt.base, tt.kind)
			resp := postProvider(t, srv.URL, "/providers/check/model", body)
			defer resp.Body.Close()
			var got providerModelCheck
			if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Status != tt.status || got.Reason != tt.reason || got.HTTPStatus != tt.httpStatus {
				t.Fatalf("check = %+v, want %s/%s/%d", got, tt.status, tt.reason, tt.httpStatus)
			}
		})
	}
}

func TestClassifyProviderModelCheckForLocalEndpointRefusals(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusUnprocessableEntity} {
		got, reason, code := classifyProviderModelCheck(&provider.APIError{Status: status, Body: strings.Repeat("x", 3)})
		if got != "unknown" || reason != "rejected" || code != status {
			t.Fatalf("status %d = %s/%s/%d", status, got, reason, code)
		}
	}
}

func TestModelCheckDetailIsPrintableRedactedAndBounded(t *testing.T) {
	const key = "sk-live-AbCd1234EfGh5678"
	withKey := func() string { return key }
	apiBody := func(body string) error { return &provider.APIError{Status: 400, Body: body} }
	escapedKey, _ := json.Marshal(key + `"\n`)
	tests := []struct {
		name    string
		err     error
		apiKey  func() string
		want    string
		without []string
	}{
		{"upstream text passes through", apiBody(`model "clef:27b" does not support tools`), withKey, `model "clef:27b" does not support tools`, nil},
		{"no upstream words", errors.New("boom"), withKey, "", nil},
		{"exact key", apiBody("bad key " + key + " for you"), withKey, "", []string{key}},
		{"url-escaped key", apiBody("?k=" + url.QueryEscape(key+" &=")), func() string { return key + " &=" }, "", []string{"AbCd1234"}},
		{"json-escaped key", apiBody(`{"echo":` + string(escapedKey) + `}`), func() string { return key + `"\n` }, "", []string{"AbCd1234"}},
		{"bearer echo", apiBody("Authorization: Bearer sk-other-ZyXw9876VuTs5432"), func() string { return "" }, "", []string{"ZyXw9876"}},
		{"query token", apiBody("GET /v1?token=abc123SECRETvalue&x=1"), func() string { return "" }, "", []string{"abc123SECRETvalue"}},
		{"cookie", apiBody("Cookie: session=Qw3rTy9988Uu1Ii; theme=dark"), func() string { return "" }, "", []string{"Qw3rTy9988Uu1Ii"}},
		{"stream payload", &provider.StreamPayloadError{Message: "rejected " + key}, withKey, "", []string{key}},
		{"typesafe body", &typesafe.HTTPError{Status: 400, Body: "echo " + key}, withKey, "", []string{key}},
		{"auth body", &provider.AuthError{Status: 401, Body: "key " + key}, withKey, "", []string{key}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := modelCheckDetail(tt.err, tt.apiKey)
			if tt.want != "" && got != tt.want {
				t.Fatalf("detail = %q, want %q", got, tt.want)
			}
			if tt.want == "" && len(tt.without) > 0 && got == "" {
				t.Fatal("detail is empty; the surrounding words must survive redaction")
			}
			for _, secret := range tt.without {
				if strings.Contains(got, secret) {
					t.Fatalf("detail %q leaks %q", got, secret)
				}
			}
		})
	}
}

func TestModelCheckDetailMasksAPlainKeyNoPatternRecognises(t *testing.T) {
	got := modelCheckDetail(&provider.APIError{Status: 400, Body: "bad hunter-two here"}, func() string { return "hunter-two" })
	if strings.Contains(got, "hunter") || !strings.HasPrefix(got, "bad ") || !strings.HasSuffix(got, " here") {
		t.Fatalf("detail = %q", got)
	}
}

func TestModelCheckDetailShortKeyDoesNotBlankTheText(t *testing.T) {
	got := modelCheckDetail(&provider.APIError{Status: 400, Body: "an error"}, func() string { return "e" })
	if got != "an error" {
		t.Fatalf("detail = %q", got)
	}
}

func TestModelCheckDetailNeutralisesControlCharacters(t *testing.T) {
	body := "line one\r\nline\ttwo \x1b[31mred\x1b[0m \x1b]0;title\x07end\x00"
	got := modelCheckDetail(&provider.APIError{Status: 400, Body: body}, func() string { return "" })
	if got != "line one line two red end" {
		t.Fatalf("detail = %q", got)
	}
}

func TestModelCheckDetailIsCutAfterRedaction(t *testing.T) {
	const key = "sk-live-AbCd1234EfGh5678"
	body := strings.Repeat("x ", 149) + key
	got := modelCheckDetail(&provider.APIError{Status: 400, Body: body}, func() string { return key })
	if strings.Contains(got, "sk-live") || strings.Contains(got, "AbCd") {
		t.Fatalf("a cut left part of the key: %q", got)
	}
	long := modelCheckDetail(&provider.APIError{Status: 400, Body: strings.Repeat("é", 5000)}, func() string { return "" })
	if r := []rune(long); len(r) != modelCheckDetailRunes+1 || r[len(r)-1] != '…' {
		t.Fatalf("detail has %d runes, want %d plus an ellipsis", len(r), modelCheckDetailRunes)
	}
	huge := modelCheckDetail(&typesafe.HTTPError{Status: 400, Body: strings.Repeat("y", 4<<20)}, func() string { return "" })
	if len([]rune(huge)) != modelCheckDetailRunes+1 {
		t.Fatalf("a 4 MB body gave %d runes", len([]rune(huge)))
	}
}

func TestModelCheckDetailBoundaryNeverLeavesAKeyPrefix(t *testing.T) {
	const key = "sk-live-AbCd1234EfGh5678"
	fillers := []string{" ", "\n", "\t \r\n", "\x1b[0m "}
	for _, filler := range fillers {
		for offset := 1; offset < len(key); offset++ {
			pad := strings.Repeat(filler, (modelCheckDetailScan-offset)/len(filler)+1)
			pad = pad[:modelCheckDetailScan-offset]
			for _, lead := range []string{"", "error: "} {
				body := lead + pad + key + " tail"
				for _, k := range []func() string{func() string { return key }, func() string { return "" }} {
					got := modelCheckDetail(&typesafe.HTTPError{Status: 400, Body: body}, k)
					if strings.Contains(got, "sk-") || strings.Contains(got, "AbCd") {
						t.Fatalf("filler %q offset %d lead %q leaked a key fragment: %q", filler, offset, lead, got)
					}
				}
			}
		}
	}
}
