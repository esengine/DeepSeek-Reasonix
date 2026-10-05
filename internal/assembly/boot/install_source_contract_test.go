package boot

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
)

// The two-phase rule only binds the model if the model is told about it, and
// the tool contract assembled here is the only place it is told. Refusing an
// unticketed apply is covered where it happens (installsource); what this pins
// is that the sentence describing it survives into the assembled runtime,
// because a tool description is edited far more often than a refusal path.
func TestInstallSourceContractStatesTheTicketRule(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	ctrl, err := Build(context.Background(), Options{
		SessionDir: filepath.Join(robustTempDir(t), "sessions"),
		TokenMode:  TokenModeFull,
		Sink:       event.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()

	for _, entry := range ctrl.AllToolContractEntries() {
		if entry.Name != "install_source" {
			continue
		}
		if !strings.Contains(entry.Description, "planId") {
			t.Fatalf("install_source description never names the ticket an apply must carry:\n%s", entry.Description)
		}
		if !strings.Contains(string(entry.Schema), "Required with apply=true") {
			t.Fatalf("planId schema does not say it is required to apply:\n%s", entry.Schema)
		}
		return
	}
	t.Fatal("install_source is not in the assembled tool contract")
}
