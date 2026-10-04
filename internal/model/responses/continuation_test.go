package responses

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"reasonix/internal/contract/provider"
)

func TestContinuationRecovery(t *testing.T) {
	for _, tc := range []struct {
		name, rejection           string
		failRetry, accepts, stale bool
	}{
		{name: "plain", rejection: "previous_response_id is not available for this user"},
		{name: "json", rejection: `{"error":{"message":"previous_response_id is not available for this user"}}`},
		{name: "unrelated prose", rejection: "invalid request"},
		{name: "both fail", rejection: "invalid request", failRetry: true},
		{name: "accepted", accepts: true},
		{name: "stale", rejection: `{"error":{"code":"previous_response_not_found"}}`, stale: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var bodies [][]byte
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				payload, _ := io.ReadAll(r.Body)
				var body map[string]any
				_ = json.Unmarshal(payload, &body)
				mu.Lock()
				bodies = append(bodies, payload)
				n := len(bodies)
				mu.Unlock()
				if !tc.accepts && (body["previous_response_id"] != nil && (!tc.stale && !tc.failRetry || n == 2) || tc.failRetry && n == 3) {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = io.WriteString(w, tc.rejection)
					return
				}
				writeEvents(w, `{"type":"response.output_text.delta","item_id":"msg","delta":"answer"}`,
					`{"type":"response.completed","response":{"id":"resp","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`)
			}))
			defer srv.Close()
			cfg := Config{Name: "relay", BaseURL: srv.URL, Model: "m"}
			p := New(cfg)
			req := provider.Request{Messages: []provider.Message{{Role: provider.RoleSystem, Content: "stable"}, {Role: provider.RoleUser, Content: "one"}}}
			collect(t, p, req)
			req.Messages = append(req.Messages, provider.Message{Role: provider.RoleAssistant, Content: "answer"}, provider.Message{Role: provider.RoleUser, Content: "two"})
			if tc.failRetry {
				_, err := p.Stream(context.Background(), req)
				var apiErr *provider.APIError
				if !errors.As(err, &apiErr) || apiErr.Status != 400 {
					t.Fatalf("retry error = %v", err)
				}
				var recoveryErr *provider.ContinuationRecoveryError
				if !errors.As(err, &recoveryErr) {
					t.Fatalf("retry lost its host-owned recovery identity: %v", err)
				}
				if len(bodies) != 3 {
					t.Fatalf("requests = %d, want 3", len(bodies))
				}
				collect(t, p, req)
				req.Messages = append(req.Messages, provider.Message{Role: provider.RoleAssistant, Content: "answer"}, provider.Message{Role: provider.RoleUser, Content: "three"})
				collect(t, p, req)
			} else {
				collect(t, p, req)
				if !tc.accepts {
					cfg.Mode = "stateless"
					expected, _, _ := New(cfg).(*client).buildRequestBody(req)
					want, _ := json.Marshal(expected)
					if len(bodies) != 3 || !bytes.Equal(bodies[2], want) {
						t.Fatalf("retry differs from stateless request: got %q, want %q", bodies, want)
					}
				}
				req.Messages = append(req.Messages, provider.Message{Role: provider.RoleAssistant, Content: "answer"}, provider.Message{Role: provider.RoleUser, Content: "three"})
				collect(t, p, req)
				var next map[string]any
				_ = json.Unmarshal(bodies[len(bodies)-1], &next)
				if (next["previous_response_id"] != nil) != (tc.accepts || tc.stale) {
					t.Fatalf("next turn continuation = %v", next["previous_response_id"])
				}
				p.(*client).ResetContext()
				collect(t, p, req)
				req.Messages = append(req.Messages, provider.Message{Role: provider.RoleAssistant, Content: "answer"}, provider.Message{Role: provider.RoleUser, Content: "four"})
				collect(t, p, req)
			}
			var last map[string]any
			_ = json.Unmarshal(bodies[len(bodies)-1], &last)
			wantContinuation := tc.accepts || tc.failRetry || tc.stale
			if (last["previous_response_id"] != nil) != wantContinuation {
				t.Fatalf("last continuation = %v, want %v", last["previous_response_id"], wantContinuation)
			}
			fresh := New(Config{Name: "relay", BaseURL: srv.URL, Model: "m"})
			collect(t, fresh, provider.Request{Messages: req.Messages[:2]})
			body, used, _ := fresh.(*client).buildRequestBody(provider.Request{Messages: req.Messages[:4]})
			if !used {
				t.Fatalf("rebuilt client did not restore continuation: %v", body)
			}
		})
	}
}

func TestFailedRecoveryStreamDoesNotDisableContinuation(t *testing.T) {
	for _, terminal := range []string{
		`{"type":"response.failed","response":{"error":{"code":"invalid_request","message":"rejected history"}}}`,
		`{"type":"response.incomplete","response":{"incomplete_details":{"reason":"max_output_tokens"}}}`,
	} {
		t.Run(terminal, func(t *testing.T) {
			attempts := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts++
				if attempts == 2 {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if attempts == 3 {
					writeEvents(w, terminal)
					return
				}
				writeEvents(w, `{"type":"response.output_text.delta","item_id":"msg","delta":"answer"}`,
					`{"type":"response.completed","response":{"id":"resp"}}`)
			}))
			defer srv.Close()
			p := New(Config{BaseURL: srv.URL, Model: "m"}).(*client)
			req := provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "one"}}}
			collect(t, p, req)
			req.Messages = append(req.Messages, provider.Message{Role: provider.RoleAssistant, Content: "answer"}, provider.Message{Role: provider.RoleUser, Content: "two"})
			out, err := p.Stream(t.Context(), req)
			if err != nil {
				t.Fatal(err)
			}
			for range out {
			}
			if attempts != 3 {
				t.Fatalf("requests = %d, want initial, continuation, retry", attempts)
			}
			collect(t, p, req)
			req.Messages = append(req.Messages, provider.Message{Role: provider.RoleAssistant, Content: "answer"}, provider.Message{Role: provider.RoleUser, Content: "three"})
			_, used, _ := p.buildRequestBody(req)
			if !used {
				t.Fatal("failed or incomplete recovery disabled continuation")
			}
		})
	}
}

func TestBadRequestWithoutContinuationIsNotRetried(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()
	p := New(Config{BaseURL: srv.URL, Model: "m"})
	_, err := p.Stream(context.Background(), provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "one"}}})
	var apiErr *provider.APIError
	if !errors.As(err, &apiErr) || requests != 1 {
		t.Fatalf("requests=%d error=%v", requests, err)
	}
}
