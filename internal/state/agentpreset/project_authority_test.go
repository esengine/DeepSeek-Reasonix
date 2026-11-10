package agentpreset

import (
	"errors"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/agentcomposition"
)

func TestProjectLaunchOverridesCannotReuseAnOldTrustedBinding(t *testing.T) {
	store := New(testenv.TempDir(t), testenv.TempDir(t))
	d := agentcomposition.Builtins()[0]
	d.ID = "project:legacy-launch"
	d.MCP = agentcomposition.Selection{Mode: agentcomposition.Select, Members: []agentcomposition.Module{{ID: "authorized", Parameters: []byte(`{"args":["other-package"]}`)}}}
	for _, target := range []string{"declaration", "resolved"} {
		t.Run(target, func(t *testing.T) {
			version, resolved := agentcomposition.Clone(d), agentcomposition.Clone(d)
			if target == "declaration" {
				resolved.MCP.Members[0].Parameters = nil
			} else {
				version.MCP.Members[0].Parameters = nil
			}
			binding := &agentcomposition.Binding{Version: agentcomposition.Version{Definition: version, Revision: agentcomposition.Revision(version)}, Resolved: resolved, Workspace: store.Workspace}
			binding.Seal()
			if err := binding.Validate(); err != nil {
				t.Fatalf("legacy history no longer readable: %v", err)
			}
			if err := writeJSON(filepath.Join(store.trustedDirectory(), "bindings", binding.Digest+".json"), binding); err != nil {
				t.Fatal(err)
			}
			if err := store.VerifyBinding(binding); !errors.Is(err, agentcomposition.ErrInvalid) {
				t.Fatalf("legacy authorization permitted a project launch override: %v", err)
			}
			if err := store.RetainBinding(binding); !errors.Is(err, agentcomposition.ErrInvalid) {
				t.Fatalf("unsafe binding retained again: %v", err)
			}
		})
	}
	if _, err := store.Save(d, ""); !errors.Is(err, agentcomposition.ErrInvalid) {
		t.Fatalf("unsafe declaration saved: %v", err)
	}
	if err := writeJSON(filepath.Join(store.directory("project"), "definitions", "legacy-launch.json"), d); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(d.ID); !errors.Is(err, agentcomposition.ErrInvalid) {
		t.Fatalf("repository file bypassed authority validation: %v", err)
	}
	version := agentcomposition.Version{Definition: d, Revision: agentcomposition.Revision(d)}
	if err := writeJSON(filepath.Join(store.directory("user"), "revisions", version.Revision+".json"), version); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Version(version.Revision); !errors.Is(err, agentcomposition.ErrInvalid) {
		t.Fatalf("old revision bypassed authority validation: %v", err)
	}
}
