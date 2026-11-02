package boot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/hostaudit"
	"reasonix/internal/contract/provider"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/store"
)

type fileStateProofProvider struct{ restartReviewProvider }

func (p *fileStateProofProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	if phaseOf(req) == "PHASE-2" {
		req.Tools = nil
	}
	return p.restartReviewProvider.Stream(ctx, req)
}

type fileStateProofAudit struct {
	mu     sync.Mutex
	values []hostaudit.ReadinessAudit
}

func (*fileStateProofAudit) Emit(event.Event) {}
func (s *fileStateProofAudit) RecordReadinessAudit(v hostaudit.ReadinessAudit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values = append(s.values, v)
}

func TestEffectCarriedProofTracksFileState(t *testing.T) {
	for _, change := range []string{"unchanged", "content", "mode"} {
		t.Run(change, func(t *testing.T) {
			isolateConfigHome(t)
			writeUserConfig(t, "[sandbox]\nbash = \"off\"\n")
			dir := robustTempDir(t)
			t.Chdir(dir)
			rec := &fileStateProofProvider{}
			kind := uniqueKind("metadata-proof-probe")
			provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
			writeFile(t, dir, "reasonix.toml", `default_model = "test-model"
[agent]
system_prompt = "BASE"
goal_token_budget = 8000
[codegraph]
enabled = false
[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
`)
			writeFile(t, dir, "REASONIX.md", "## Reasonix host checks\n\n- verify: git diff --check\n- sensitive: auth/**\n")
			gitInDir(t, dir, "init", "-q")
			approveWorkspace(t, dir)
			audit := &fileStateProofAudit{}
			build := func() *control.Controller {
				c, err := Build(context.Background(), Options{Sink: audit, AgentPreset: AgentPresetDelivery})
				if err != nil {
					t.Fatal(err)
				}
				c.SetToolApprovalMode(control.ToolApprovalYolo)
				return c
			}
			c := build()
			path := filepath.Join(dir, ".reasonix", "sessions", "metadata-proof.jsonl")
			c.SetSessionPath(path)
			c.SetGoal("change the auth login")
			submitAndSettle(t, c, "PHASE-1 implement the login change", &rec.restartReviewProvider)
			c.Close()
			raw, err := os.ReadFile(store.SessionGoalState(path))
			if err != nil {
				t.Fatal(err)
			}
			var saved struct {
				DeliveryCheckpoint evidence.DeliveryCheckpoint `json:"deliveryCheckpoint"`
			}
			if err := json.Unmarshal(raw, &saved); err != nil {
				t.Fatal(err)
			}
			cp := saved.DeliveryCheckpoint
			if !cp.PendingMutation || cp.Proven == nil || !cp.Proven.Verified || !cp.Proven.SignedOff || !cp.Proven.Inspected {
				t.Fatalf("producer did not persist usable proof: %+v proven=%+v", cp, cp.Proven)
			}
			file := filepath.Join(dir, "auth", "login.go")
			beforeInfo, err := os.Stat(file)
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "content":
				if err := os.WriteFile(file, append(before, []byte("// outside edit\n")...), 0o644); err != nil {
					t.Fatal(err)
				}
			case "mode":
				defer func() { _ = os.Chmod(file, beforeInfo.Mode().Perm()) }()
				if err := os.Chmod(file, 0o444); err != nil {
					t.Fatal(err)
				}
				afterInfo, err := os.Stat(file)
				if err != nil || afterInfo.Mode() == beforeInfo.Mode() {
					t.Fatalf("fixture did not change file permissions: %v", err)
				}
				after, err := os.ReadFile(file)
				if err != nil || string(after) != string(before) {
					t.Fatal("permission fixture changed content")
				}
			}
			loaded, err := sessionstore.LoadSession(path)
			if err != nil || loaded == nil {
				t.Fatalf("load: %v", err)
			}
			c = build()
			defer c.Close()
			if err := c.Resume(loaded, path); err != nil {
				t.Fatal(err)
			}
			audit.mu.Lock()
			audit.values = nil
			audit.mu.Unlock()
			c.ResumeGoal()
			submitAndSettle(t, c, "PHASE-2 finish without another check", &rec.restartReviewProvider)
			audit.mu.Lock()
			values := append([]hostaudit.ReadinessAudit(nil), audit.values...)
			audit.mu.Unlock()
			if len(values) == 0 {
				t.Fatal("no resumed readiness observations")
			}
			missing := false
			for _, v := range values {
				missing = missing || v.MissingVerification > 0 || v.MissingPathInspection > 0 || v.MissingSignoff > 0
			}
			t.Logf("change=%s persistedProof=%+v resumedAudits=%+v", change, cp.Proven, values)
			if missing != (change != "unchanged") {
				t.Fatalf("change=%s fresh proof demanded=%v", change, missing)
			}
		})
	}
}
