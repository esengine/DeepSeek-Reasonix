package agent

import (
	"context"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

type fixedHostContext []string

func (f fixedHostContext) RequestContext() []string { return f }

func TestHostContextReachesTheRequestOnce(t *testing.T) {
	for _, window := range []int{0, 100_000} {
		prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{{{Type: provider.ChunkText, Text: "ok"}, {Type: provider.ChunkDone}}}}
		a := New(prov, tool.NewRegistry(), sessionstore.NewSession(""), Options{ArchiveDir: testenv.TempDir(t), ContextWindow: window}, event.Discard)
		a.SetHostContext(fixedHostContext{"<interrupted-adjudication>a run stopped</interrupted-adjudication>"})
		if err := a.Run(context.Background(), "hi"); err != nil {
			t.Fatalf("Run: %v", err)
		}
		n := 0
		for _, m := range prov.requests[0].Messages {
			n += strings.Count(m.Content, "<interrupted-adjudication>")
		}
		if n != 1 {
			t.Fatalf("window %d: host context block reached the request %d times, want once", window, n)
		}
	}
}
