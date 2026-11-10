package sessionstore

import (
	"errors"
	"os"
	"path/filepath"
	"reasonix/internal/base/testenv"
	"testing"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/provider"
)

func TestMoveCannotDiscardACompositionBinding(t *testing.T) {
	for _, started := range []bool{false, true} {
		t.Run(map[bool]string{false: "blank", true: "started"}[started], func(t *testing.T) {
			from, to := testenv.TempDir(t), testenv.TempDir(t)
			src := filepath.Join(from, "20261018-000000-bound.jsonl")
			d := agentcomposition.Builtins()[1]
			binding := &agentcomposition.Binding{Version: agentcomposition.Version{Definition: d, Revision: agentcomposition.Revision(d)}, Resolved: d, Workspace: from}
			binding.Seal()
			s := NewSession("bound prompt")
			s.BindComposition(binding)
			if started {
				s.Add(provider.Message{Role: provider.RoleUser, Content: "keep this conversation bound"})
			}
			if err := s.SaveSnapshot(src); err != nil {
				t.Fatal(err)
			}
			moved, err := MoveSession(src, to, to)
			if !errors.Is(err, agentcomposition.ErrWorkspaceBound) || moved != "" {
				t.Fatalf("move discarded the immutable binding: path=%q err=%v", moved, err)
			}
			original, err := LoadSession(src)
			if err != nil || original.Composition() == nil || original.Composition().Digest != s.Composition().Digest {
				t.Fatalf("refusal changed the source binding: %v", err)
			}
			entries, err := os.ReadDir(to)
			if err != nil || len(entries) != 0 {
				t.Fatalf("refusal published a target: %v %v", entries, err)
			}
		})
	}
}
