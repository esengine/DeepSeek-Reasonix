//go:build darwin || linux

package boot

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/base/secrets"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/team"
	"reasonix/internal/state/teamroster"
)

// A secret the user gave an agent reaches its shell through the real assembly,
// and no request to the provider ever carries the value: what the command
// printed comes back masked, and so would anything else that echoed it.
func TestEffectATeamSecretReachesTheShellAndNeverTheProvider(t *testing.T) {
	home := statedBootHome(t)
	t.Setenv("REASONIX_HOME", home)
	dir := testenv.TempDir(t)
	store := teamroster.Store{Dir: testenv.TempDir(t)}
	var ada string
	if _, err := store.Update(context.Background(), func(r *teamroster.Roster) error {
		ada = r.AddAgent(teamroster.Agent{Name: "Ada", Main: true, CreatedBy: teamroster.ByUser}).ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	svc := team.New(team.Options{Store: store})
	const value = "sk-shop-0123456789"
	if err := svc.UserSaveSecret(context.Background(), "SHOP_KEY", value, "the shop", []string{ada}, ""); err != nil {
		t.Fatal(err)
	}
	defer secrets.UnregisterValue(value)

	rec := &egressScriptProvider{command: `printf 'key=%s' "$REASONIX_SECRET_SHOP_KEY"`}
	provider.Register("boot-team-secret", func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"
tool_approval = "yolo"

[agent]
system_prompt = "BASE"

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "boot-team-secret"
model = "x"
`)
	approveWorkspaceAt(t, home, dir)
	ctrl, err := Build(context.Background(), Options{Home: home, WorkspaceRoot: dir, Sink: event.Discard, TeamSeat: team.Seat{Service: svc, Ref: team.SeatRef{Agent: ada}}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "my shop key is "+value+", use it"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	rec.mu.Lock()
	reqs := append([]provider.Request(nil), rec.reqs...)
	rec.mu.Unlock()
	results := effectToolResults(reqs[len(reqs)-1])
	if len(results) != 1 || !strings.Contains(results[0], "key=[secret:SHOP_KEY]") {
		t.Fatalf("the command did not read the secret, or printed it unmasked: %q", results)
	}
	for i, r := range reqs {
		for _, m := range r.Messages {
			for _, s := range []string{m.Content, m.ReasoningContent} {
				if strings.Contains(s, value) {
					t.Fatalf("request %d carried the secret in a %s message", i, m.Role)
				}
			}
			for _, c := range m.ToolCalls {
				if strings.Contains(c.Arguments, value) {
					t.Fatalf("request %d carried the secret in a tool call", i)
				}
			}
		}
	}
}
