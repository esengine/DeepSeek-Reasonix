package browser

import (
	"context"
	"encoding/json"

	"reasonix/internal/tool"
)

// Names lists the tool names in the order Tools returns them.
func Names() []string {
	return []string{
		"browser_tabs", "browser_open", "browser_navigate", "browser_snapshot", "browser_screenshot",
		"browser_click", "browser_type", "browser_press", "browser_scroll", "browser_select", "browser_upload",
		"browser_download", "browser_close",
	}
}

// Tools builds the thirteen browser tools over exec. A nil exec still yields
// every tool, each reporting ProviderVisible false so it fails closed.
func Tools(exec Executor) []tool.Tool {
	return []tool.Tool{
		tabsTool(exec), openTool(exec), navigateTool(exec), snapshotTool(exec), screenshotTool(exec),
		clickTool(exec), typeTool(exec), pressTool(exec), scrollTool(exec), selectTool(exec), uploadTool(exec),
		downloadTool(exec), closeTool(exec),
	}
}

type runFunc func(ctx context.Context, exec Executor, args json.RawMessage) (string, error)

// base carries one tool's identity and its availability check.
type base struct {
	exec        Executor
	name        string
	description string
	schema      json.RawMessage
	snip        tool.SnipHint
}

func (b base) Name() string            { return b.name }
func (b base) Description() string     { return b.description }
func (b base) Schema() json.RawMessage { return b.schema }
func (b base) SnipHint() tool.SnipHint { return b.snip }
func (b base) ProviderVisible(ctx context.Context) bool {
	if b.exec == nil {
		return false
	}
	if a, ok := b.exec.(Availability); ok {
		return a.Available(ctx)
	}
	return true
}

func (b base) ready(ctx context.Context) error {
	if b.exec == nil {
		return tool.Blocked(noBrowserText)
	}
	if !b.ProviderVisible(ctx) {
		return tool.Blocked(noGrantText)
	}
	return ctx.Err()
}

type readTool struct {
	base
	run runFunc
}

func (readTool) ReadOnly() bool     { return true }
func (readTool) PlanModeSafe() bool { return true }
func (t readTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if err := t.ready(ctx); err != nil {
		return "", err
	}
	return t.run(ctx, t.exec, args)
}

type writeTool struct {
	base
	run runFunc
}

func (writeTool) ReadOnly() bool     { return false }
func (writeTool) PlanModeSafe() bool { return false }
func (t writeTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if err := t.ready(ctx); err != nil {
		return "", err
	}
	return t.run(ctx, t.exec, args)
}

// EffectHint names the tab a write touches, or the URL when no tab exists
// yet (browser_open); every write reaches the network and none is destructive.
func (writeTool) EffectHint(args json.RawMessage) tool.EffectHint {
	var p struct {
		TabID string `json:"tabId"`
		URL   string `json:"url"`
	}
	_ = json.Unmarshal(args, &p)
	hint := tool.EffectHint{Known: true, UsesNetwork: true}
	switch {
	case p.TabID != "":
		hint.Targets = []string{p.TabID}
	case p.URL != "":
		hint.Targets = []string{p.URL}
	}
	return hint
}

var (
	shortSnip = tool.SnipHint{Head: 8, Tail: 4, HeadChars: 800, TailChars: 400}
	listSnip  = tool.SnipHint{Head: 40, Tail: 8, HeadChars: 4000, TailChars: 800}
	treeSnip  = tool.SnipHint{Head: 200, Tail: 20, HeadChars: 16000, TailChars: 2000}
)
