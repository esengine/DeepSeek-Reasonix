package market

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestOwnFailedInstallCanBePreviewedAgainAfterCorrectingTheTarget(t *testing.T) {
	f, _ := ownFixture(t, "private", "")
	preview := func() Outcome {
		t.Helper()
		out, err := f.svc.PlanOwn(t.Context(), "tok", ownReq("acme/review-kit", "", "", ""))
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	apply := func(plan Outcome) Outcome {
		t.Helper()
		out, err := f.svc.InstallOwn(t.Context(), "tok", ownReq("acme/review-kit", "1.0.0", planID(t, plan), digestOf(t, plan)))
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if out := apply(preview()); string(out.Fields["status"]) != `"done"` {
		t.Fatalf("first install = %s", out.Fields["status"])
	}
	before, err := os.ReadFile(f.skill)
	if err != nil {
		t.Fatal(err)
	}
	consumed := preview()
	failed := apply(consumed)
	if string(failed.Fields["ok"]) != "false" || string(failed.Fields["applied"]) != "true" || string(failed.Fields["status"]) != `"failed"` {
		t.Fatalf("duplicate install fields = %+v", failed.Fields)
	}
	var actions []struct {
		Status string `json:"status"`
		Error  string `json:"error"`
		Next   string `json:"next"`
	}
	if err := json.Unmarshal(failed.Fields["actions"], &actions); err != nil || len(actions) != 1 || actions[0].Status != "failed" || actions[0].Error == "" || actions[0].Next == "" {
		t.Fatalf("failed actions = %+v, %v", actions, err)
	}
	if got, err := os.ReadFile(f.skill); err != nil || string(got) != string(before) {
		t.Fatalf("failed install changed the original skill: %v", err)
	}
	if err := os.RemoveAll(filepath.Dir(f.skill)); err != nil {
		t.Fatal(err)
	}
	fresh := preview()
	if string(fresh.Fields["status"]) != `"planned"` || digestOf(t, fresh) != digestOf(t, consumed) {
		t.Fatalf("fresh preview = %+v, want the unchanged source pinned again", fresh.Fields)
	}
	if out := apply(fresh); string(out.Fields["status"]) != `"done"` || !out.Unreviewed {
		t.Fatalf("recovered install = %s, unreviewed = %v", out.Fields["status"], out.Unreviewed)
	}
	if got, err := os.ReadFile(f.skill); err != nil || string(got) != string(before) {
		t.Fatalf("recovered skill differs from the pinned source: %v", err)
	}
}
