package boot

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/surface"
	"reasonix/internal/session/team"
	"reasonix/internal/state/stats"
	"reasonix/internal/state/teamroster"
)

// What a team agent's seat spends is recorded under that agent through the
// real assembly, so the usage page can say who spent it; a session off the
// team records none.
func TestEffectATeamSeatsUsageIsRecordedUnderItsAgent(t *testing.T) {
	home := statedBootHome(t)
	dir := testenv.TempDir(t)
	store := teamroster.Store{Dir: testenv.TempDir(t)}
	var ada string
	if _, err := store.Update(context.Background(), func(r *teamroster.Roster) error {
		ada = r.AddAgent(teamroster.Agent{Name: "Ada", Main: true, CreatedBy: teamroster.ByUser}).ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	chat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"done\"},\"finish_reason\":null}]}\n\n")
		fmt.Fprint(w, "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":1,\"total_tokens\":11}}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(chat.Close)
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "openai"
base_url = "`+chat.URL+`"
model = "x"
api_key = "test-key"
`)
	approveWorkspaceAt(t, home, dir)
	run := func(seat team.Seat, opts Options) {
		t.Helper()
		opts.Home, opts.WorkspaceRoot, opts.Sink, opts.StatsSource = home, dir, event.Discard, surface.Desktop
		if seat.Service != nil {
			opts.TeamSeat = seat
		}
		ctrl, err := Build(context.Background(), opts)
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		defer ctrl.Close()
		if err := ctrl.Run(context.Background(), "hello"); err != nil {
			t.Fatalf("Run: %v", err)
		}
	}
	run(team.Seat{Service: team.New(team.Options{Store: store}), Ref: team.SeatRef{Agent: ada}}, Options{})
	run(team.Seat{}, Options{})

	statsDir := config.RootsForHome(home).StatsDir()
	if err := stats.Flush(context.Background(), statsDir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(statsDir, time.Now().Format("2006-01-02")+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var withAgent, usage int
	for line := range bytes.Lines(data) {
		if !bytes.Contains(line, []byte(`"total":11`)) {
			continue
		}
		usage++
		if bytes.Contains(line, []byte(`"agent":"`+ada+`"`)) {
			withAgent++
		}
	}
	if usage != 2 || withAgent != 1 {
		t.Fatalf("%d usage records, %d under Ada, want 2 and 1:\n%s", usage, withAgent, data)
	}
}
