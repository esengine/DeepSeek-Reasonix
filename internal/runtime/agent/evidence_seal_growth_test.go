package agent

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/runtime/completion"
	"reasonix/internal/runtime/taskcontract"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/trustedstate"
)

func storeBytes(t *testing.T, root string) int64 {
	t.Helper()
	var total int64
	err := filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err == nil {
			total += info.Size()
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return total
}

// A workspace with thousands of captured tests puts them in the contract
// revision and nowhere else: a turn that changes no criterion seals a bundle
// that names the revision and adds nothing proportional to the criteria.
func TestSealedBundleDoesNotCopyTheContractEachTurn(t *testing.T) {
	root := t.TempDir()
	store := trustedstate.Open(root, nil)
	a, sink := contractAgent(t, &scriptedProvider{}, "", store)
	a.deliveryProfile = true
	a.task.baselineCriteria = map[string]evidence.TestCriterion{}
	for i := range 3000 {
		path := fmt.Sprintf("/work/tree/benchmarks/e2e/tasks/case-%04d/workdir/pkg/order_test.go", i)
		a.task.baselineCriteria[path] = evidence.TestCriterion{LogicalID: path, Digest: evidence.DigestOf([]byte(path))}
	}
	seal := func(input string) {
		a.sealShadowBundle(input, taskcontract.New(input), completion.Report{}, nil, false)
	}

	seal("first")
	first := storeBytes(t, root)
	seal("second")
	added := storeBytes(t, root) - first

	rec, err := store.Record(trustedstate.Digest(sink.last(t).Record))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := store.Object(rec.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) > 16<<10 {
		t.Fatalf("bundle payload is %d bytes for 3000 criteria; it must name the contract, not copy it", len(payload))
	}
	if strings.Contains(string(payload), "order_test.go") {
		t.Fatal("bundle repeats criterion identities already held by the contract revision")
	}
	if added > 32<<10 {
		t.Fatalf("a turn that changed no criterion added %d bytes to the store", added)
	}
}
