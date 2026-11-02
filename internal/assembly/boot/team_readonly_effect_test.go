package boot

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/session/team"
	"reasonix/internal/state/teamroster"
)

func TestEffectReadOnlyChildCannotDispatchTeamWork(t *testing.T) {
	requireTeamExecution(t)
	for _, dynamic := range []bool{false, true} {
		name := "direct"
		if dynamic {
			name = "capability"
		}
		t.Run(name, func(t *testing.T) {
			home, dir := statedBootHome(t), robustTempDir(t)
			store := teamroster.Store{Dir: t.TempDir()}
			var chief string
			if _, err := store.Update(t.Context(), func(r *teamroster.Roster) error {
				chief = r.AddAgent(teamroster.Agent{Name: "Chief", Main: true, CreatedBy: teamroster.ByUser}).ID
				r.AddAgent(teamroster.Agent{Name: "Ben", CreatedBy: teamroster.ByUser})
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			svc := team.New(team.Options{Store: store, Panes: &teamEffectPanes{}})
			parent := &browserScriptProvider{rounds: []func(string) *provider.ToolCall{
				func(string) *provider.ToolCall {
					return browserCall("delegate", "use_capability", map[string]any{
						"action": "call", "capability_id": "tool:read_only_task",
						"arguments": map[string]any{"prompt": "Inspect the team without starting work", "model": "worker"},
					})
				},
			}}
			child := &browserScriptProvider{rounds: []func(string) *provider.ToolCall{
				func(string) *provider.ToolCall {
					args := map[string]any{"to": "Ben", "message": "Write the release files"}
					if dynamic {
						return browserCall("child-dispatch", "use_capability", map[string]any{
							"action": "call", "capability_id": "tool:message_agent", "arguments": args,
						})
					}
					return browserCall("child-dispatch", "message_agent", args)
				},
			}}
			kind := "team-readonly-" + name
			provider.Register(kind, func(cfg provider.Config) (provider.Provider, error) {
				if cfg.Model == "worker" {
					return child, nil
				}
				return parent, nil
			})
			writeFile(t, dir, "reasonix.toml", `
default_model = "parent"
[codegraph]
enabled = false
[[providers]]
name = "parent"
kind = "`+kind+`"
model = "parent"
[[providers]]
name = "worker"
kind = "`+kind+`"
model = "worker"
`)
			approveWorkspaceAt(t, home, dir)
			ctrl, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: dir, Sink: event.Discard,
				TeamSeat: team.Seat{Service: svc, Ref: team.SeatRef{Agent: chief}}})
			if err != nil {
				t.Fatal(err)
			}
			defer ctrl.Close()
			ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
			if err := ctrl.Run(context.Background(), "Delegate a read-only inspection"); err != nil {
				t.Fatal(err)
			}
			reqs := child.requests()
			if len(reqs) < 2 {
				t.Fatalf("child did not attempt the call: %d requests", len(reqs))
			}
			book, err := store.Exchanges()
			if err != nil || len(book) != 0 {
				t.Fatalf("read-only child dispatched team work under its parent's identity: %+v, %v", book, err)
			}
			results := effectToolResults(reqs[len(reqs)-1])
			if len(results) != 1 || strings.Contains(results[0], "Delivered to") {
				t.Fatalf("expected a refusal, got %q", results)
			}
			for _, requests := range [][]provider.Request{parent.requests(), reqs} {
				first, err := json.Marshal(requests[0].Tools)
				if err != nil {
					t.Fatal(err)
				}
				for _, req := range requests[1:] {
					next, err := json.Marshal(req.Tools)
					if err != nil || !bytes.Equal(first, next) || systemOf(req) != systemOf(requests[0]) {
						t.Fatalf("team execution boundary changed the prefix across turns: %v", err)
					}
				}
			}
		})
	}
}
