package boot

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

type failingOnceProvider struct {
	mu    sync.Mutex
	calls int
	reqs  []provider.Request
}

func (p *failingOnceProvider) Name() string { return "fail-once" }

func (p *failingOnceProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	call := p.calls
	p.calls++
	p.reqs = append(p.reqs, req)
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 2)
	if call == 0 {
		return nil, fmt.Errorf("scripted provider failure")
	}
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "ok"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestZZDebtOnFailedTurn(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	prov := &failingOnceProvider{}
	provider.Register("fail-once", func(provider.Config) (provider.Provider, error) { return prov, nil })
	writeFile(t, dir, "reasonix.toml", "\ndefault_model = \"test-model\"\n\n[agent]\nsystem_prompt = \"BASE\"\n\n[[providers]]\nname = \"test-model\"\nkind = \"fail-once\"\nmodel = \"x\"\n")
	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()

	err = ctrl.Run(context.Background(), "turn-alpha")
	t.Logf("first turn err=%v", err)
	inHistory := false
	for _, m := range ctrl.History() {
		if m.Role == provider.RoleUser && strings.Contains(m.Content, "turn-alpha") {
			inHistory = strings.Contains(m.Content, "<available-skills>")
		}
	}
	t.Logf("failed turn's composed message still in session with the block: %v", inHistory)

	if err := ctrl.Run(context.Background(), "turn-beta"); err != nil {
		t.Fatal(err)
	}
	prov.mu.Lock()
	last := prov.reqs[len(prov.reqs)-1]
	prov.mu.Unlock()
	sawBlock := false
	for _, m := range last.Messages {
		if m.Role == provider.RoleUser && strings.Contains(m.Content, "<available-skills>") {
			sawBlock = true
		}
	}
	t.Logf("the model's next request carries a skills block at all: %v", sawBlock)
}
