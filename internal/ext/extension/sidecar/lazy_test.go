package sidecar

import (
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/extension/protocol"
	"reasonix/internal/ext/pluginpkg"
)

func TestToolOnlyRuntimeIsDeferredWithoutCopyingPackage(t *testing.T) {
	p := pluginpkg.InstalledPackage{}
	p.Installed.Name = "lazy-tools"
	p.Package.Root = "/nonexistent-package-not-read-at-selection"
	p.Package.Manifest.Runtime = &pluginpkg.RuntimeSpec{
		Command: "/must-not-start",
		Tools:   []pluginpkg.RuntimeTool{{Name: "echo"}},
	}
	m, _, err := StartLivePackages(t.Context(), testenv.TempDir(t), []pluginpkg.InstalledPackage{p}, protocol.SessionContext{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if len(m.Clients()) != 0 || len(m.DeferredPackages()) != 1 || len(m.privateRoots) != 0 {
		t.Fatal("tool-only package was launched, copied or omitted")
	}
}
