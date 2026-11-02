package delegation

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/contract/provider"
)

func TestSubagentPersistedStatusAdmission(t *testing.T) {
	for _, status := range []string{"future-paused", "missing", "empty", "COMPLETED", "completed", "cancelled", "failed", "interrupted", "running"} {
		t.Run(status, func(t *testing.T) {
			for _, route := range []string{"continue", "copy", "ancestor", "legacy_ancestor", "current_copy"} {
				t.Run(route, func(t *testing.T) {
					sessionDir, store, ref, spec := prepareCompletedSubagentForLineageTest(t, "root")
					saveTestBranchMeta(t, sessionDir, "root", "")
					saveTestBranchMeta(t, sessionDir, "child", "root")
					if route == "current_copy" {
						child := spec
						child.ParentSession = "child"
						run, err := store.PrepareContinue(ref, child)
						if err != nil {
							t.Fatal(err)
						}
						if err = store.SaveCompleted(run); err != nil {
							t.Fatal(err)
						}
						run.Release()
						setAdmissionStatus(t, store.metaPath(run.Ref), status)
					} else {
						setAdmissionStatus(t, store.metaPath(ref), status)
					}
					before := admissionStoreBytes(t, store.dir)
					// Re-open to exercise persisted input, not a live run's in-memory state.
					store = NewSubagentStore(store.dir)
					if route == "ancestor" || route == "legacy_ancestor" || route == "current_copy" {
						spec.ParentSession = "child"
					}
					var next *SubagentRun
					var err error
					switch route {
					case "copy":
						next, err = store.prepareFork(ref, spec)
					case "legacy_ancestor":
						next, err = store.PrepareLegacyForkFrom(ref, spec)
					default:
						next, err = store.PrepareContinue(ref, spec)
					}
					allowed := status == "completed" || status == "cancelled"
					if next != nil {
						next.Release()
					}
					if (err == nil) != allowed {
						t.Fatalf("status=%q admitted=%v want=%v err=%v", status, err == nil, allowed, err)
					}
					if !allowed {
						after := admissionStoreBytes(t, store.dir)
						if len(after) != len(before) {
							t.Fatalf("refusal changed artifact count %d -> %d", len(before), len(after))
						}
						for name, data := range before {
							if !bytes.Equal(data, after[name]) {
								t.Errorf("refusal changed %s", name)
							}
						}
						// Refusal must release the reference lock so repair or diagnosis can proceed.
						release, lockErr := store.lock(ref)
						if lockErr != nil {
							t.Fatal(lockErr)
						}
						release()
					}
				})
			}
		})
	}
}

func setAdmissionStatus(t *testing.T, path, status string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var meta map[string]any
	if err = json.Unmarshal(raw, &meta); err != nil {
		t.Fatal(err)
	}
	switch status {
	case "missing":
		delete(meta, "status")
	case "empty":
		meta["status"] = ""
	default:
		meta["status"] = status
	}
	meta["futureField"] = map[string]any{"retained": true}
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func admissionStoreBytes(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".json" && filepath.Ext(e.Name()) != ".jsonl" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out[e.Name()] = data
	}
	return out
}

func TestSubagentUnknownStatusStillReadableForDiagnosis(t *testing.T) {
	_, store, ref, spec := prepareCompletedSubagentForLineageTest(t, "root")
	setAdmissionStatus(t, store.metaPath(ref), "future-paused")
	meta, err := store.LoadMeta(ref)
	if err != nil || meta.Status != "future-paused" {
		t.Fatalf("meta=%+v err=%v", meta, err)
	}
	if _, _, err = store.ReadResult(ref, spec.ParentSession, spec.WorkspaceRoot); err == nil {
		t.Fatal("unknown status returned a result")
	}
	if _, _, err = store.ReadFinalAnswer(ref, spec.ParentSession, spec.WorkspaceRoot); err == nil {
		t.Fatal("unknown status satisfied final answer")
	}
	// The persisted transcript itself remains available to explicit diagnosis.
	data, err := os.ReadFile(store.sessionPath(ref))
	if err != nil || !bytes.Contains(data, []byte(provider.RoleSystem)) {
		t.Fatalf("transcript retained=%v err=%v", len(data) > 0, err)
	}
}
