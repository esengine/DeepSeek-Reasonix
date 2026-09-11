package browser

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"reasonix/internal/tool"
)

const clickArgs = `{"operationId":"op-1","tabId":"t1","documentToken":"doc-1","ref":"e12"}`

var readOnlyTools = map[string]bool{
	"browser_tabs": true, "browser_snapshot": true, "browser_screenshot": true, "browser_download": true,
}

func TestNamesMatchTools(t *testing.T) {
	names := Names()
	if len(names) != 13 {
		t.Fatalf("Names() = %d entries, want 13", len(names))
	}
	tools := Tools(nil)
	if len(tools) != len(names) {
		t.Fatalf("Tools() = %d tools, Names() = %d", len(tools), len(names))
	}
	seen := map[string]bool{}
	for i, tl := range tools {
		if tl.Name() != names[i] {
			t.Errorf("tool %d = %q, Names()[%d] = %q", i, tl.Name(), i, names[i])
		}
		if seen[tl.Name()] {
			t.Errorf("duplicate tool %q", tl.Name())
		}
		seen[tl.Name()] = true
		if !strings.HasPrefix(tl.Name(), "browser_") || strings.TrimSpace(tl.Description()) == "" {
			t.Errorf("tool %q: bad name or empty description", tl.Name())
		}
	}
}

func TestReadOnlyPlanModeSnipAndEffectHints(t *testing.T) {
	for _, tl := range Tools(&fakeExecutor{}) {
		name := tl.Name()
		wantRead := readOnlyTools[name]
		if tl.ReadOnly() != wantRead {
			t.Errorf("%s ReadOnly = %v, want %v", name, tl.ReadOnly(), wantRead)
		}
		if pm, ok := tl.(tool.PlanModeClassifier); !ok || pm.PlanModeSafe() != wantRead {
			t.Errorf("%s PlanModeSafe: implemented=%v, want %v", name, ok, wantRead)
		}
		if _, ok := tl.(tool.ContextualTool); !ok {
			t.Errorf("%s does not implement ContextualTool", name)
		}
		if sh, ok := tl.(tool.SnipHinter); !ok {
			t.Errorf("%s does not implement SnipHinter", name)
		} else if h := sh.SnipHint(); h.Head <= 0 || h.Tail <= 0 || h.HeadChars <= 0 || h.TailChars <= 0 {
			t.Errorf("%s SnipHint = %+v, want positive counts", name, h)
		}
		hp, hasHint := tl.(tool.EffectHintProvider)
		if wantRead {
			if hasHint {
				t.Errorf("%s is read-only but implements EffectHintProvider", name)
			}
			continue
		}
		if !hasHint {
			t.Errorf("%s is a write but lacks EffectHintProvider", name)
			continue
		}
		h := hp.EffectHint(json.RawMessage(`{"tabId":"t1","operationId":"op-1"}`))
		if !h.Known || !h.UsesNetwork || h.Destructive || h.ReadOnly || !reflect.DeepEqual(h.Targets, []string{"t1"}) {
			t.Errorf("%s EffectHint = %+v", name, h)
		}
	}
	open := toolByName(t, nil, "browser_open").(tool.EffectHintProvider)
	if h := open.EffectHint(json.RawMessage(`{"operationId":"op","url":"https://example.com"}`)); !reflect.DeepEqual(h.Targets, []string{"https://example.com"}) {
		t.Errorf("browser_open EffectHint targets = %v", h.Targets)
	}
}

func TestProviderVisibleFollowsExecutor(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		exec Executor
		want bool
	}{
		{"nil", nil, false},
		{"plain", &fakeExecutor{}, true},
		{"revoked", gatedExecutor{&fakeExecutor{}, false}, false},
		{"granted", gatedExecutor{&fakeExecutor{}, true}, true},
	}
	for _, c := range cases {
		for _, tl := range Tools(c.exec) {
			if got := tl.(tool.ContextualTool).ProviderVisible(ctx); got != c.want {
				t.Errorf("%s: %s ProviderVisible = %v, want %v", c.name, tl.Name(), got, c.want)
			}
		}
	}
	_, err := run(t, nil, "browser_tabs", `{}`)
	if msg, ok := tool.BlockedMessage(err); !ok || !strings.Contains(msg, "no browser") {
		t.Fatalf("nil executor: err = %v, want blocked no-browser", err)
	}
	fake := &fakeExecutor{}
	_, err = run(t, gatedExecutor{fake, false}, "browser_snapshot", `{"tabId":"t1"}`)
	if msg, ok := tool.BlockedMessage(err); !ok || !strings.Contains(msg, "grant") {
		t.Fatalf("revoked grant: err = %v, want blocked no-grant", err)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("revoked grant reached the executor: %v", fake.calls)
	}
}

func TestSentinelErrorsBecomeBlocked(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{ErrStaleReference, "stale reference"},
		{ErrTakenOver, "took over"},
		{ErrNoGrant, "grant"},
	}
	for _, c := range cases {
		fake := &fakeExecutor{err: c.err}
		for _, call := range []struct{ name, args string }{{"browser_click", clickArgs}, {"browser_snapshot", `{"tabId":"t1"}`}} {
			_, err := run(t, fake, call.name, call.args)
			msg, ok := tool.BlockedMessage(err)
			if !ok || !strings.Contains(msg, c.want) {
				t.Errorf("%s with %v: err = %v, want blocked containing %q", call.name, c.err, err, c.want)
			}
		}
	}
}

func TestUnknownOutcomeIsNonRetryError(t *testing.T) {
	for _, fake := range []*fakeExecutor{{err: ErrUnknownOutcome}, {act: ActResult{Outcome: OutcomeUnknown}}} {
		_, err := run(t, fake, "browser_click", clickArgs)
		if err == nil {
			t.Fatal("unknown outcome returned no error")
		}
		if _, blocked := tool.BlockedMessage(err); blocked {
			t.Fatalf("unknown outcome rendered as blocked: %v", err)
		}
		if s := err.Error(); !strings.Contains(s, "outcome unknown") || !strings.Contains(s, "must not be retried") {
			t.Fatalf("unknown outcome text = %q", s)
		}
	}
}

func TestActOutcomes(t *testing.T) {
	fake := &fakeExecutor{act: ActResult{Outcome: OutcomeNotExecuted, Reason: "element obscured"}}
	_, err := run(t, fake, "browser_click", clickArgs)
	if err == nil || !strings.Contains(err.Error(), "not_executed") || !strings.Contains(err.Error(), "element obscured") {
		t.Fatalf("not_executed err = %v", err)
	}
	fake = &fakeExecutor{act: ActResult{Executed: true, Outcome: OutcomeExecuted, DocumentToken: "doc-2"}}
	out, err := run(t, fake, "browser_click", clickArgs)
	if err != nil || !strings.Contains(out, "executed: click e12 on tab t1") || !strings.Contains(out, "documentToken: doc-2") {
		t.Fatalf("executed out = %q, err = %v", out, err)
	}
}

func TestActRequestsReachExecutor(t *testing.T) {
	fake := &fakeExecutor{act: ActResult{Executed: true, Outcome: OutcomeExecuted}}
	common := `"operationId":"op-1","tabId":"t1","documentToken":"doc-1"`
	cases := []struct {
		name string
		args string
		want ActRequest
	}{
		{"browser_click", clickArgs, ActRequest{Action: ActionClick, Ref: "e12"}},
		{"browser_type", `{` + common + `,"ref":"e3","text":"hello","submit":true}`, ActRequest{Action: ActionType, Ref: "e3", Text: "hello", Submit: true}},
		{"browser_press", `{` + common + `,"keys":"Control+a"}`, ActRequest{Action: ActionPress, Keys: "Control+a"}},
		{"browser_scroll", `{` + common + `,"ref":"e7","deltaY":400}`, ActRequest{Action: ActionScroll, Ref: "e7", DeltaY: 400}},
		{"browser_select", `{` + common + `,"ref":"e5","options":["a","b"]}`, ActRequest{Action: ActionSelect, Ref: "e5", Options: []string{"a", "b"}}},
		{"browser_upload", `{` + common + `,"ref":"e9","files":["/tmp/x.txt"]}`, ActRequest{Action: ActionUpload, Ref: "e9", Files: []string{"/tmp/x.txt"}}},
	}
	for i, c := range cases {
		out, err := run(t, fake, c.name, c.args)
		if err != nil || !strings.HasPrefix(out, "executed: ") {
			t.Fatalf("%s: out = %q, err = %v", c.name, out, err)
		}
		want := c.want
		want.OperationID, want.TabID, want.DocumentToken = "op-1", "t1", "doc-1"
		if !reflect.DeepEqual(fake.acts[i], want) {
			t.Fatalf("%s: Act got %+v, want %+v", c.name, fake.acts[i], want)
		}
	}
}

func TestArgumentValidation(t *testing.T) {
	fake := &fakeExecutor{act: ActResult{Executed: true}}
	common := `"operationId":"op","tabId":"t1","documentToken":"d"`
	cases := []struct{ name, args, want string }{
		{"browser_click", `{"operationId":"bad id!","tabId":"t1","documentToken":"d","ref":"e1"}`, "operationId"},
		{"browser_click", `{"operationId":"","tabId":"t1","documentToken":"d","ref":"e1"}`, "operationId"},
		{"browser_click", `{"operationId":"` + strings.Repeat("a", 101) + `","tabId":"t1","documentToken":"d","ref":"e1"}`, "operationId"},
		{"browser_click", `{` + common + `}`, "ref is required"},
		{"browser_type", `{` + common + `,"text":"x"}`, "ref is required"},
		{"browser_select", `{` + common + `,"options":["a"]}`, "ref is required"},
		{"browser_upload", `{` + common + `,"files":["a"]}`, "ref is required"},
		{"browser_click", `{"operationId":"op","tabId":"t1","ref":"e1"}`, "documentToken is required"},
		{"browser_click", `{"operationId":"op","documentToken":"d","ref":"e1"}`, "tabId is required"},
		{"browser_click", `{` + common + `,"ref":"e1","text":"x"}`, `unknown field "text"`},
		{"browser_type", `{` + common + `,"ref":"e1","text":""}`, "text is required"},
		{"browser_press", `{` + common + `,"keys":" "}`, "keys is required"},
		{"browser_scroll", `{` + common + `}`, "deltaX or deltaY"},
		{"browser_select", `{` + common + `,"ref":"e1","options":[]}`, "options must list"},
		{"browser_upload", `{` + common + `,"ref":"e1","files":[""]}`, "files must not contain"},
		{"browser_navigate", `{"operationId":"op","tabId":"t1","action":"jump"}`, "action must be one of"},
		{"browser_navigate", `{"operationId":"op","tabId":"t1","action":"url"}`, "url is required when action is url"},
		{"browser_navigate", `{"operationId":"op","tabId":"t1","action":"back","url":"https://x"}`, "url is only accepted"},
		{"browser_open", `{"operationId":"op"}`, "url is required"},
		{"browser_open", `{"operationId":"op","url":"https://x","extra":1}`, "unknown field"},
		{"browser_close", `{"tabId":"t1"}`, "operationId"},
		{"browser_snapshot", `{}`, "tabId is required"},
		{"browser_download", `{"tabId":"t1","waitSeconds":301}`, "waitSeconds"},
		{"browser_tabs", `{"tabId":"t1"}`, "unknown field"},
	}
	for _, c := range cases {
		_, err := run(t, fake, c.name, c.args)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s %s: err = %v, want containing %q", c.name, c.args, err, c.want)
		}
		if _, blocked := tool.BlockedMessage(err); blocked {
			t.Errorf("%s %s: validation rendered as blocked", c.name, c.args)
		}
	}
	if len(fake.calls) != 0 {
		t.Fatalf("invalid args reached the executor: %v", fake.calls)
	}
}

func TestReadResults(t *testing.T) {
	fake := &fakeExecutor{
		tabs:      []Tab{{ID: "t1", URL: "https://a.example", Title: "A", Loading: true}, {ID: "t2", URL: "https://b.example", Temporary: true}},
		snapshot:  Snapshot{DocumentToken: "doc-7", URL: "https://a.example", Title: "A", Tree: "document\n  button \"Go\" ref=e1", Refs: 1},
		downloads: []Download{{ID: "d1", URL: "https://a.example/f.zip", Path: "/tmp/f.zip", State: "completed", Bytes: 12}},
	}
	out, err := run(t, fake, "browser_tabs", ``)
	if err != nil || !strings.Contains(out, "2 tab(s)") || !strings.Contains(out, `tab t1: https://a.example "A" [loading]`) || !strings.Contains(out, "tab t2: https://b.example [temporary]") {
		t.Fatalf("tabs out = %q, err = %v", out, err)
	}
	out, err = run(t, fake, "browser_snapshot", `{"tabId":"t1","selector":"main"}`)
	if err != nil || !strings.HasPrefix(out, "documentToken: doc-7\n") || !strings.Contains(out, "refs: 1") || !strings.Contains(out, "ref=e1") {
		t.Fatalf("snapshot out = %q, err = %v", out, err)
	}
	if !reflect.DeepEqual(fake.snaps, []SnapshotRequest{{TabID: "t1", Selector: "main"}}) {
		t.Fatalf("snapshot requests = %+v", fake.snaps)
	}
	out, err = run(t, fake, "browser_download", `{"tabId":"t1","waitSeconds":5}`)
	if err != nil || !strings.Contains(out, "download d1: completed https://a.example/f.zip -> /tmp/f.zip (12 bytes)") {
		t.Fatalf("download out = %q, err = %v", out, err)
	}
	if !reflect.DeepEqual(fake.dls, []DownloadsRequest{{TabID: "t1", WaitFor: 5 * time.Second}}) {
		t.Fatalf("download requests = %+v", fake.dls)
	}
	if out, err := run(t, &fakeExecutor{}, "browser_tabs", `{}`); err != nil || !strings.Contains(out, "no tabs are open") {
		t.Fatalf("empty tabs out = %q, err = %v", out, err)
	}
}

func TestTabWrites(t *testing.T) {
	fake := &fakeExecutor{}
	out, err := run(t, fake, "browser_open", `{"operationId":"op-1","url":"https://c.example","temporary":true}`)
	if err != nil || !strings.Contains(out, "opened tab t-new: https://c.example [temporary]") || !strings.Contains(out, "browser_snapshot") {
		t.Fatalf("open out = %q, err = %v", out, err)
	}
	if !reflect.DeepEqual(fake.opens, []OpenRequest{{OperationID: "op-1", URL: "https://c.example", Temporary: true}}) {
		t.Fatalf("open requests = %+v", fake.opens)
	}
	out, err = run(t, fake, "browser_navigate", `{"operationId":"op-2","tabId":"t1","action":"reload"}`)
	if err != nil || !strings.Contains(out, "navigated (reload)") || !strings.Contains(out, "now invalid") {
		t.Fatalf("navigate out = %q, err = %v", out, err)
	}
	if !reflect.DeepEqual(fake.navs, []NavigateRequest{{OperationID: "op-2", TabID: "t1", Action: NavigateReload}}) {
		t.Fatalf("navigate requests = %+v", fake.navs)
	}
	out, err = run(t, fake, "browser_close", `{"operationId":"op-3","tabId":"t2"}`)
	if len(fake.closes) != 1 || fake.closes[0].OperationID != "op-3" {
		t.Fatalf("close operationId lost: %+v", fake.closes)
	}
	if err != nil || out != "closed tab t2" || !reflect.DeepEqual(fake.closed, []string{"t2"}) {
		t.Fatalf("close out = %q, err = %v, closed = %v", out, err, fake.closed)
	}
}
