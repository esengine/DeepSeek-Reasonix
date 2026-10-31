package providerbroker

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/base/secrets/secretstest"
	"reasonix/internal/contract/provider"
)

// The broker's own process never holds the person's secrets, so the request
// leaves this one already masked.
func TestABrokeredRequestLeavesMasked(t *testing.T) {
	const value = "sk-broker-0123456789"
	secretstest.Hold("BROKER_KEY", value)
	seen := make(chan provider.Request, 1)
	fake := &fakeProvider{name: "p", seen: seen, script: []provider.Chunk{{Type: provider.ChunkDone}}}
	client := newPair(t, staticResolver(fake, provider.Descriptor{Ref: "p/m"}))
	p, err := client.Resolve(provider.Selection{Ref: "p/m"})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := p.Stream(context.Background(), provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "use " + value}}})
	if err != nil {
		t.Fatal(err)
	}
	drain(t, ch)
	if sent := <-seen; strings.Contains(sent.Messages[0].Content, value) || sent.Messages[0].Content != "use [secret:BROKER_KEY]" {
		t.Fatalf("the broker received %q", sent.Messages[0].Content)
	}
}
