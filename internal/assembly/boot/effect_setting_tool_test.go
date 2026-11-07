package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

// settingProvider asks for one setting change on its first request and then
// ends the turn, recording what each request carried.
type settingProvider struct {
	mu   sync.Mutex
	reqs []provider.Request
	id   string
	val  string
}

func (p *settingProvider) Name() string { return "boot-setting-tool" }

func (p *settingProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	first := len(p.reqs) == 1
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 3)
	if first {
		args, _ := json.Marshal(map[string]any{"action": "call", "capability_id": "tool:setting", "arguments": map[string]any{"op": "propose", "id": p.id, "value": json.RawMessage(p.val), "reason": "the task needs it"}})
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "call-set", Name: "use_capability", Arguments: string(args)}}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *settingProvider) toolResult() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.reqs) < 2 {
		return ""
	}
	for _, m := range p.reqs[1].Messages {
		if m.Role == provider.RoleTool && m.Name == "use_capability" {
			return m.Content
		}
	}
	return ""
}

func (p *settingProvider) toolNames(i int) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var names []string
	for _, t := range p.reqs[i].Tools {
		names = append(names, t.Name)
	}
	return names
}

func settingSession(t *testing.T, id, val string) (*settingProvider, *control.Controller, string, chan event.Approval) {
	t.Helper()
	home := statedBootHome(t)
	dir := testenv.TempDir(t)
	rec := &settingProvider{id: id, val: val}
	kind := "boot-setting-tool-" + strings.ReplaceAll(t.Name(), "/", "-")
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeFile(t, dir, "reasonix.toml", fmt.Sprintf("default_model = \"test-model\"\n\n[codegraph]\nenabled = false\n\n[[providers]]\nname = \"test-model\"\nkind = %q\nmodel = \"x\"\n", kind))
	approveWorkspaceAt(t, home, dir)
	approvals := make(chan event.Approval, 4)
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.ApprovalRequest {
			approvals <- e.Approval
		}
	})
	ctrl, err := Build(context.Background(), Options{Home: home, WorkspaceRoot: dir, Sink: sink})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(ctrl.Close)
	if err := ctrl.NewSession(); err != nil {
		t.Fatal(err)
	}
	ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
	ctrl.EnableInteractiveApproval()
	path := config.UserConfigPath()
	cfg := config.LoadForEdit(path)
	cfg.Peers.Enabled, cfg.Permissions.Mode = false, ""
	if err := cfg.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	return rec, ctrl, path, approvals
}

func runSetting(ctrl *control.Controller) chan error {
	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		done <- ctrl.Run(ctx, "get the other project to do it")
	}()
	return done
}

// A model that asks to loosen a switch gets a card even in YOLO, nothing is
// written until the click, and the next request carries the same tool block.
// The process's user config is shared, so these run one after another.
func TestEffectTheModelProposesASettingAndTheUserClickDecidesIt(t *testing.T) {
	for _, allow := range []bool{true, false} {
		t.Run(fmt.Sprintf("allow=%v", allow), func(t *testing.T) {
			rec, ctrl, userFile, approvals := settingSession(t, "peers.enabled", "true")
			done := runSetting(ctrl)
			var card event.Approval
			select {
			case card = <-approvals:
			case <-time.After(20 * time.Second):
				t.Fatal("no card for a proposed setting")
			}
			if card.Tool != control.SettingElevatedApprovalTool || !card.Fresh || card.AllowsSession {
				t.Fatalf("card = %+v", card)
			}
			if config.LoadForEdit(userFile).Peers.Enabled {
				t.Fatal("the file changed before the click")
			}
			ctrl.Approve(card.ID, allow, false, false)
			if err := <-done; err != nil {
				t.Fatalf("Run: %v", err)
			}
			if got := config.LoadForEdit(userFile).Peers.Enabled; got != allow {
				t.Fatalf("peers.enabled = %v after allow=%v", got, allow)
			}
			result := rec.toolResult()
			want := `"via":"click"`
			if !allow {
				want = "setting.declined"
			}
			if !strings.Contains(result, want) {
				t.Fatalf("tool result = %q, want %q", result, want)
			}
			if !reflect.DeepEqual(rec.toolNames(0), rec.toolNames(1)) {
				t.Fatalf("the tool block moved between requests:\n%v\n%v", rec.toolNames(0), rec.toolNames(1))
			}
			if slices.Contains(rec.toolNames(0), "setting") {
				t.Fatalf("setting is in the first-round schema: %v", rec.toolNames(0))
			}
		})
	}
}

// A boundary setting is never proposed: no card, nothing written, and the
// refusal tells the model the one thing it may do instead.
func TestEffectTheModelCannotProposeAGuardedSetting(t *testing.T) {
	rec, ctrl, userFile, approvals := settingSession(t, "permissions.mode", `"yolo"`)
	if err := <-runSetting(ctrl); err != nil {
		t.Fatalf("Run: %v", err)
	}
	select {
	case a := <-approvals:
		t.Fatalf("a card for a boundary setting: %+v", a)
	default:
	}
	result := rec.toolResult()
	for _, want := range []string{"setting.guarded", "(fix: tool:setting op=open id=permissions.mode)"} {
		if !strings.Contains(result, want) {
			t.Fatalf("tool result = %q, want %q", result, want)
		}
	}
	if config.LoadForEdit(userFile).Permissions.Mode == "yolo" {
		t.Fatal("a guarded setting was written")
	}
}
