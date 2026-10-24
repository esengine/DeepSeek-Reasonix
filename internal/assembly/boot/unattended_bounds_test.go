package boot

import (
	"errors"
	"testing"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/checkenv"
)

func TestUnattendedOptionsPreserveProcessBounds(t *testing.T) {
	network := false
	parent := Options{WorkspaceRoot: "/source", WorkspaceOnly: true, SandboxNetworkOverride: &network,
		SandboxBashOverride: "required", PermissionAllow: []string{"read_file"}, AdditionalDirs: []string{"/authorized"}, VerificationClient: checkenv.ClientSpec{Executable: "/host/docker", Environment: []string{"DOCKER_HOST=unix:///host/engine.sock"}}}
	child, err := unattendedOptions(parent, event.Discard, "ask", "/candidate", "model")
	if err != nil {
		t.Fatal(err)
	}
	if !child.WorkspaceOnly || child.SandboxNetworkOverride == nil || *child.SandboxNetworkOverride || child.SandboxBashOverride != "required" {
		t.Fatalf("child lost process bounds: %+v", child)
	}
	if child.WorkspaceRoot != "/candidate" || child.FileOverlay != nil || child.TerminalRunner != nil || child.WorkspaceLease != nil {
		t.Fatal("candidate retained source I/O owners")
	}
	child.PermissionAllow[0] = "bash"
	if child.VerificationClient.Executable != parent.VerificationClient.Executable || child.VerificationClient.Environment[0] != parent.VerificationClient.Environment[0] {
		t.Fatal("child lost host-selected verification client")
	}
	child.VerificationClient.Environment[0] = "DOCKER_HOST=changed"
	if parent.VerificationClient.Environment[0] != "DOCKER_HOST=unix:///host/engine.sock" {
		t.Fatal("child changed parent verification client")
	}
	child.AdditionalDirs[0] = "/changed"
	if parent.PermissionAllow[0] != "read_file" || parent.AdditionalDirs[0] != "/authorized" {
		t.Fatal("child modified parent grants")
	}
}

func TestUnattendedNeverSilentlyDropsComposition(t *testing.T) {
	for _, parent := range []Options{{AgentComposition: "restricted"}, {Composition: &agentcomposition.Binding{}}} {
		_, err := unattendedOptions(parent, event.Discard, "auto", "/candidate", "model")
		var refusal tool.Refusal
		if !errors.As(err, &refusal) || refusal.Code != "isolation.unsupported_arg" {
			t.Fatalf("composition: %v", err)
		}
	}
}
