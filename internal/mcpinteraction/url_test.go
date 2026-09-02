package mcpinteraction

import (
	"context"
	"encoding/json"
	"testing"
)

func TestAllowedURLModes(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{"https://example.com/oauth", true},
		{"http://localhost:8931/callback", true},
		{"https://example.com/cb?state=xyz", true},
		{"", false},
		{"file:///etc/passwd", false},
		{"javascript:alert(1)", false},
		{"ftp://example.com/x", false},
		{"https://user:pass@example.com/", false},
		{"https://example.com/cb?password=secret", false},
		{"not a url", false},
		{"https://", false},
	}
	for _, tc := range cases {
		if got := allowedURL(tc.raw); got != tc.want {
			t.Errorf("allowedURL(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

func TestSanitizeURLModeIgnoresFormMode(t *testing.T) {
	req := Request{Mode: ModeForm, Message: "fill this"}
	if !SanitizeURLMode(req) {
		t.Fatal("form mode must not be URL-gated")
	}
	req = Request{Mode: ModeURL, URL: "javascript:alert(1)"}
	if SanitizeURLMode(req) {
		t.Fatal("dangerous url mode must be refused")
	}
}

type recordingBroker struct {
	got Request
	res Result
}

func (b *recordingBroker) Interact(_ context.Context, req Request) (Result, error) {
	b.got = req
	return b.res, nil
}

func TestBrokerContextRoundTrip(t *testing.T) {
	broker := &recordingBroker{res: Result{Action: ActionAccept, Content: map[string]any{"q": "a"}}}
	ctx := WithBroker(context.Background(), broker)
	got := FromContext(ctx)
	if got == nil {
		t.Fatal("broker not retrievable from context")
	}
	schema, _ := json.Marshal(map[string]any{"type": "object"})
	res, err := got.Interact(ctx, Request{ID: "1", Server: "srv", Mode: ModeForm, RequestedSchema: schema})
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != ActionAccept || res.Content["q"] != "a" {
		t.Fatalf("result = %+v", res)
	}
	if broker.got.ID != "1" || broker.got.Server != "srv" {
		t.Fatalf("request = %+v", broker.got)
	}
	if FromContext(context.Background()) != nil {
		t.Fatal("empty context must resolve to nil broker")
	}
	if FromContext(nil) != nil { //nolint:staticcheck // Explicitly verify the documented nil-safe boundary.
		t.Fatal("nil context must resolve to nil broker")
	}
}
