package market

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFailedPublicInstallCanReadCurrentStateAndPreviewAgain(t *testing.T) {
	f := newFixture(t)
	preview := func() Outcome {
		t.Helper()
		out, err := f.svc.Plan(t.Context(), Request{Slug: "acme/review-kit"})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	apply := func(plan Outcome) Outcome {
		t.Helper()
		out, err := f.svc.Install(t.Context(), Request{Slug: "acme/review-kit", Version: "1.0.0", PlanID: planID(t, plan)})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if out := apply(preview()); string(out.Fields["status"]) != `"done"` {
		t.Fatalf("first install=%+v", out.Fields)
	}
	before, err := os.ReadFile(f.skill)
	if err != nil {
		t.Fatal(err)
	}
	failed := apply(preview())
	if string(failed.Fields["ok"]) != "false" || string(failed.Fields["status"]) != `"failed"` {
		t.Fatalf("returned failure=%+v", failed.Fields)
	}
	if got, err := os.ReadFile(f.skill); err != nil || string(got) != string(before) {
		t.Fatalf("failed install rewrote skill: %v", err)
	}
	if err := os.RemoveAll(filepath.Dir(f.skill)); err != nil {
		t.Fatal(err)
	}
	if records := InstalledRecords(f.home); len(records) != 0 {
		t.Fatalf("removed target remains installed: %+v", records)
	}
	if out := apply(preview()); string(out.Fields["status"]) != `"done"` {
		t.Fatalf("fresh install=%+v", out.Fields)
	}
	if got, err := os.ReadFile(f.skill); err != nil || string(got) != string(before) {
		t.Fatalf("recovered skill differs: %v", err)
	}
}
