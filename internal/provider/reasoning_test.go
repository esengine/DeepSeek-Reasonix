package provider_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"reasonix/internal/provider"
	_ "reasonix/internal/provider/anthropic"
	_ "reasonix/internal/provider/openai"
	_ "reasonix/internal/provider/responses"
)

func TestAdapterReasoningExactSelectionBeforeIO(t *testing.T) {
	for _, kind := range []string{"openai", "anthropic", "responses"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			var body map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				// A terminal response exercises egress without invoking any external model.
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"message":"contract fixture"}}`))
			}))
			defer server.Close()
			p, err := provider.New(kind, provider.Config{Name: "fixture", BaseURL: server.URL, Model: "exact-model", APIKey: "fixture", Extra: map[string]any{
				"request_url": server.URL, "reasoning_protocol": "deepseek", "thinking": "adaptive",
				"supported_efforts": []string{"low", "high", "max"}, "effort": "high",
			}})
			if err != nil {
				t.Fatal(err)
			}
			declared, ok := p.(provider.ReasoningProvider)
			if !ok {
				t.Fatal("adapter does not expose reasoning capability")
			}
			cap := declared.ReasoningCapability()
			if !reflect.DeepEqual(cap.IDs(), []string{"low", "high", "max"}) {
				t.Fatalf("options: %+v", cap)
			}
			cap.Options[0].ID = "corrupted"
			if declared.ReasoningCapability().IDs()[0] != "low" {
				t.Fatal("mutable capability escaped adapter")
			}
			for _, bad := range []string{"medium", "HIGH", " high ", "auto", "off"} {
				_, err = p.Stream(context.Background(), provider.Request{EffortOverride: bad})
				var unsupported *provider.UnsupportedReasoningEffort
				if !errors.As(err, &unsupported) {
					t.Fatalf("%q: expected typed error, got %v", bad, err)
				}
				if calls != 0 {
					t.Fatalf("unsupported %q reached network", bad)
				}
			}
			for _, level := range declared.ReasoningCapability().IDs() {
				_, _ = p.Stream(context.Background(), provider.Request{EffortOverride: level})
				var got any
				switch kind {
				case "openai":
					got = body["reasoning_effort"]
				case "anthropic":
					got = body["output_config"].(map[string]any)["effort"]
				case "responses":
					got = body["reasoning"].(map[string]any)["effort"]
				}
				if got != level {
					t.Fatalf("selected %q, emitted %v", level, got)
				}
			}
			if calls != 3 {
				t.Fatalf("calls=%d", calls)
			}
		})
	}
}
