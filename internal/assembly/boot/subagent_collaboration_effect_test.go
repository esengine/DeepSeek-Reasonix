package boot

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/sessionstore"
)

type collaborationProbe struct {
	isolated bool
	mu       sync.Mutex
	requests []provider.Request
}

func (*collaborationProbe) Name() string { return "collaboration-effect" }
func (p *collaborationProbe) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	i := len(p.requests)
	p.requests = append(p.requests, req)
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 2)
	calls := []provider.ToolCall{
		{ID: "delegate", Name: "use_capability", Arguments: `{"action":"call","capability_id":"task:subagent","arguments":{"prompt":"Inspect source.txt and report","context_summary":"The parser must preserve empty fields","tools":["read_file"]}}`},
		{ID: "reply", Name: "reply_to_parent", Arguments: `{"message":"I found the parser; checking empty fields now"}`},
		{ID: "read", Name: "read_file", Arguments: `{"path":"source.txt"}`},
	}
	if i < len(calls) {
		if i == 0 && p.isolated {
			calls[0].Arguments = strings.Replace(calls[0].Arguments, `"prompt":"Inspect`, `"isolation":"worktree","prompt":"Inspect`, 1)
		}
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &calls[i]}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "Inspection complete."}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestEffectSubagentReplyAndSelectedContextKeepPrefixesStable(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	p := &collaborationProbe{}
	provider.Register(p.Name(), func(provider.Config) (provider.Provider, error) { return p, nil })
	writeFile(t, dir, "reasonix.toml", `default_model = "probe"
[agent]
system_prompt = "CACHE-STABLE-BASE"
[[providers]]
name = "probe"
kind = "collaboration-effect"
model = "probe"
`)
	writeFile(t, dir, "source.txt", "PRIVATE-CHILD-READ")
	approveWorkspace(t, dir)
	c, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Run(context.Background(), "Inspect the parser using a child"); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.requests) != 5 {
		t.Fatalf("requests = %d, want parent, three child rounds, parent", len(p.requests))
	}
	opening := p.requests[1]
	if !requestContains(opening, "The parser must preserve empty fields") {
		t.Fatal("selected context never reached the child")
	}
	if !requestContains(p.requests[2], "Report queued") || !requestContains(p.requests[3], "PRIVATE-CHILD-READ") {
		t.Fatal("child did not continue work after reporting")
	}
	parent := p.requests[4]
	if !requestContains(parent, "I found the parser") || !requestContains(parent, sessionstore.HostNoticePrefix) || requestContains(parent, "PRIVATE-CHILD-READ") {
		t.Fatal("parent did not receive an attributed report, or inherited child tool chatter")
	}
	for _, pair := range [][2]int{{0, 4}, {1, 2}, {1, 3}} {
		a, b := p.requests[pair[0]], p.requests[pair[1]]
		if !reflect.DeepEqual(systemMessages(a), systemMessages(b)) || !reflect.DeepEqual(a.Tools, b.Tools) {
			t.Fatalf("system or tools changed between requests %v", pair)
		}
		if len(b.Messages) < len(a.Messages) || !reflect.DeepEqual(a.Messages, b.Messages[:len(a.Messages)]) {
			t.Fatalf("existing message prefix changed between requests %v", pair)
		}
	}
	for _, req := range p.requests {
		prefix, _ := json.Marshal(struct {
			System []provider.Message
			Tools  any
		}{systemMessages(req), req.Tools})
		for _, dynamic := range []string{"The parser must preserve empty fields", "I found the parser", "PRIVATE-CHILD-READ"} {
			if strings.Contains(string(prefix), dynamic) {
				t.Fatalf("dynamic context entered prefix: %s", dynamic)
			}
		}
	}
}

func requestContains(req provider.Request, text string) bool {
	for _, message := range req.Messages {
		if strings.Contains(message.Content, text) {
			return true
		}
	}
	return false
}

func systemMessages(req provider.Request) []provider.Message {
	var messages []provider.Message
	for _, message := range req.Messages {
		if message.Role == provider.RoleSystem {
			messages = append(messages, message)
		}
	}
	return messages
}
