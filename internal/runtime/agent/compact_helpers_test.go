package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"slices"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

// foldableSessionOverForce builds a transcript whose bulk is assistant text, so
// the free prune pass cannot reclaim it and Prepare must reach the summarizer.
func foldableSessionOverForce(turns int) *sessionstore.Session {
	big := strings.Repeat("word ", 400)
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "standing constraint: never change the public API"},
	}
	for range turns {
		msgs = append(msgs,
			provider.Message{Role: provider.RoleAssistant, Content: big},
			provider.Message{Role: provider.RoleUser, Content: "continue"},
		)
	}
	return &sessionstore.Session{Messages: msgs}
}

func agentOverForce(t *testing.T, prov provider.Provider, sess *sessionstore.Session) *Agent {
	t.Helper()
	return agentOverForceWindow(t, prov, sess, 5000)
}

// agentOverForceWindow sits the session above the force ratio. A folded
// transcript lands back under the trigger, so a blocked turn can only mean the
// fold itself failed.
func agentOverForceWindow(t *testing.T, prov provider.Provider, sess *sessionstore.Session, window int) *Agent {
	t.Helper()
	return New(prov, tool.NewRegistry(), sess, Options{
		ContextWindow: window,
		CompactRatio:  0.5,
		ArchiveDir:    testenv.TempDir(t),
	}, event.Discard)
}

func prepareContext(ctx context.Context, a *Agent, trigger string) error {
	_, err := a.window().contextManager().Prepare(ctx, ContextPreparePolicy{Trigger: trigger})
	return err
}

// latestDigest returns the text of the last compaction digest in a projection.
func latestDigest(msgs []provider.Message) string {
	for _, m := range slices.Backward(msgs) {
		if isCompactionSummary(m) {
			return m.Content
		}
	}
	return ""
}

type scriptedReply struct {
	text  string
	usage *provider.Usage
	fail  error
}

// scriptedSummarizer answers each summarizer call from its own script, so a
// test can bill the two halves of a repair differently and tell them apart.
type scriptedSummarizer struct {
	mu      sync.Mutex
	replies []scriptedReply
	calls   int
}

func (p *scriptedSummarizer) Name() string { return "scripted" }

func (p *scriptedSummarizer) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	at := p.calls
	p.calls++
	p.mu.Unlock()
	if at >= len(p.replies) {
		return nil, fmt.Errorf("unscripted summarizer call %d", at+1)
	}
	r := p.replies[at]
	ch := make(chan provider.Chunk, 4)
	if r.text != "" {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: r.text}
	}
	if r.usage != nil {
		ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: r.usage}
	}
	if r.fail != nil {
		ch <- provider.Chunk{Type: provider.ChunkError, Err: r.fail}
	}
	close(ch)
	return ch, nil
}

func billed(prompt, completion int) *provider.Usage {
	return &provider.Usage{PromptTokens: prompt, CompletionTokens: completion, TotalTokens: prompt + completion}
}

// archivedFolds counts the archive files this agent has written.
func archivedFolds(a *Agent) int {
	files, _ := filepath.Glob(filepath.Join(a.window().outputsDir(), "fold-*.md"))
	return len(files)
}
