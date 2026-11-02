package boot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/base/secrets"
	"reasonix/internal/base/secrets/secretstest"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	_ "reasonix/internal/model/responses"
	"reasonix/internal/session/team"
	"reasonix/internal/state/teamroster"
)

// Every wire a provider entry may speak masks a value the host holds for the
// person in the bytes it puts on the wire, whichever path built the request:
// the agent loop, a title, a memory review or an advisor all reach a model
// through one of these. A kind absent from the protocol table cannot be saved.
func TestEveryProviderKindMasksSecretsOnTheWire(t *testing.T) {
	const value = "sk-wire-0123456789abcdef"
	secretstest.Hold("WIRE_KEY", value)
	for _, proto := range config.Protocols() {
		if proto.Answers != config.AnswersChat {
			continue
		}
		kind := proto.Kind
		t.Run(kind, func(t *testing.T) {
			var mu sync.Mutex
			var bodies []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data, _ := io.ReadAll(r.Body)
				mu.Lock()
				bodies = append(bodies, string(data))
				mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"message":"captured","type":"invalid_request_error"}}`))
			}))
			defer srv.Close()
			p, err := newProviderForTest(&config.ProviderEntry{Name: "wire", Kind: kind, BaseURL: srv.URL, Model: "m"})
			if err != nil {
				t.Fatalf("kind %s does not build from a bare entry: %v", kind, err)
			}
			ch, err := p.Stream(context.Background(), provider.Request{Tools: []provider.ToolSchema{{
				Name: "fixture", Description: "Use " + value,
				Parameters: json.RawMessage(`{"type":"object","properties":{"token":{"type":"string","default":"\u0073k-wire-0123456789abcdef"}}}`),
			}}, Messages: []provider.Message{
				{Role: provider.RoleSystem, Content: "you hold " + value},
				{Role: provider.RoleUser, Content: "use " + value},
			}})
			if err == nil {
				for range ch {
				}
			}
			mu.Lock()
			defer mu.Unlock()
			if len(bodies) == 0 {
				t.Fatalf("kind %s sent nothing (err %v)", kind, err)
			}
			for _, b := range bodies {
				var decoded any
				if err := json.Unmarshal([]byte(b), &decoded); err != nil {
					t.Fatalf("kind %s sent invalid JSON: %v", kind, err)
				}
				normalized, _ := json.Marshal(decoded)
				if strings.Contains(string(normalized), value) || !strings.Contains(b, "[secret:WIRE_KEY]") {
					t.Fatalf("kind %s put %s on the wire", kind, b)
				}
			}
		})
	}
}

// A process that reaches a model without building a session, as review does,
// still masks what the team stored: building the provider is enough.
func TestBuildingAProviderAloneMasksTheTeamsSecrets(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	const value = "sk-review-0123456789"
	store := team.WithSecretCredentials(teamroster.Store{Dir: config.RootsForHome("").TeamDir()})
	if err := os.MkdirAll(store.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := store.EditSecrets(context.Background(), func(b *teamroster.SecretBook) error {
		b.Secrets = append(b.Secrets, teamroster.Secret{Name: "REVIEW_KEY", Value: value})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := newProviderForTest(&config.ProviderEntry{Name: "review", Kind: "openai", BaseURL: "http://127.0.0.1:1", Model: "m"}); err != nil {
		t.Fatal(err)
	}
	if got := secrets.Masker().String("diff adds " + value); got != "diff adds [secret:REVIEW_KEY]" {
		t.Fatalf("after building a provider = %q", got)
	}
}
