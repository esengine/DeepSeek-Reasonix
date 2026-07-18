package profileconfig

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"reasonix/internal/remote/catalog"
	"reasonix/internal/remote/protocol"
)

func writeProfileConfig(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func profileFixture(t *testing.T) (home, workspace string) {
	t.Helper()
	home = t.TempDir()
	workspace = t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_SAFE_MODE", "")
	t.Setenv("REMOTE_PROFILE_UNSET_KEY", "")
	writeProfileConfig(t, filepath.Join(home, "config.toml"), `default_model = "user/user-chat"

[desktop]
default_tool_approval_mode = "auto"
provider_access = ["user", "project", "locked"]

[[providers]]
name = "user"
kind = "openai"
base_url = "http://127.0.0.1:18080/v1"
models = ["user-chat", "text-embedding-private"]
default = "user-chat"
reasoning_protocol = "openai"

[[providers]]
name = "locked"
kind = "openai"
base_url = "https://api.openai.com/v1"
model = "locked-chat"
api_key_env = "REMOTE_PROFILE_UNSET_KEY"
`)
	writeProfileConfig(t, filepath.Join(workspace, "reasonix.toml"), `default_model = "project/project-chat"

# A repository may choose its model, but it must not escalate the user-global
# default approval posture for a newly-created interactive Desktop Session.
[desktop]
default_tool_approval_mode = "yolo"

[[providers]]
name = "project"
kind = "openai"
base_url = "http://127.0.0.1:18081/v1"
models = ["project-chat", "project-alt"]
default = "project-chat"
supported_efforts = ["low", "high"]
default_effort = "high"
`)
	return home, workspace
}

func TestResolveProfileUsesLayeredHostConfigAndFreezesEveryDefaultAxis(t *testing.T) {
	_, workspace := profileFixture(t)
	profile, err := New().ResolveProfile(context.Background(), workspace, protocol.ProfileSelection{})
	if err != nil {
		t.Fatal(err)
	}
	want := protocol.ResolvedProfile{
		Model: "project/project-chat", Effort: "high",
		CollaborationMode: protocol.CollaborationNormal,
		TokenMode:         protocol.TokenFull, ToolApprovalMode: protocol.ToolApprovalAuto,
	}
	if profile != want {
		t.Fatalf("resolved profile = %+v, want %+v", profile, want)
	}
}

func TestResolveProfileCanonicalizesExplicitSelectionWithExistingSemantics(t *testing.T) {
	_, workspace := profileFixture(t)
	model := "project-alt" // bare model refs use Config.ResolveModel
	effort := " HIGH "
	collaboration := protocol.CollaborationPlan
	token := protocol.TokenEconomy
	approval := protocol.ToolApprovalAuto
	profile, err := New().ResolveProfile(context.Background(), workspace, protocol.ProfileSelection{
		Model: &model, Effort: &effort, CollaborationMode: &collaboration,
		TokenMode: &token, ToolApprovalMode: &approval,
	})
	if err != nil {
		t.Fatal(err)
	}
	if profile.Model != "project/project-alt" || profile.Effort != "high" ||
		profile.CollaborationMode != protocol.CollaborationPlan || profile.TokenMode != protocol.TokenEconomy ||
		profile.ToolApprovalMode != protocol.ToolApprovalAuto {
		t.Fatalf("resolved explicit profile = %+v", profile)
	}
}

func TestResolveProfileFreezesExplicitAutoToCurrentEffortDefault(t *testing.T) {
	_, workspace := profileFixture(t)
	auto := "auto"
	profile, err := New().ResolveProfile(context.Background(), workspace, protocol.ProfileSelection{Effort: &auto})
	if err != nil {
		t.Fatal(err)
	}
	if profile.Effort != "high" {
		t.Fatalf("resolved auto effort = %q, want current Host default high", profile.Effort)
	}
}

func TestResolveProfilePreservesLegacyDesktopAxesDuringCatalogMigration(t *testing.T) {
	_, workspace := profileFixture(t)
	legacyMode := protocol.CollaborationMode("plan-yolo")
	legacyToken := protocol.TokenMode("balanced")
	profile, err := New().ResolveProfile(context.Background(), workspace, protocol.ProfileSelection{
		CollaborationMode: &legacyMode, TokenMode: &legacyToken,
	})
	if err != nil {
		t.Fatal(err)
	}
	if profile.CollaborationMode != protocol.CollaborationPlan || profile.TokenMode != protocol.TokenFull ||
		profile.ToolApprovalMode != protocol.ToolApprovalYOLO {
		t.Fatalf("legacy axes = %+v", profile)
	}
}

func TestResolveProfileReturnsFrozenErrorCodes(t *testing.T) {
	_, workspace := profileFixture(t)
	tests := []struct {
		name      string
		selection protocol.ProfileSelection
		want      protocol.ReasonixErrorCode
	}{
		{name: "unknown model", selection: profileModel("missing/model"), want: protocol.ErrModelNotAvailable},
		{name: "provider without credentials", selection: profileModel("locked/locked-chat"), want: protocol.ErrModelNotAvailable},
		{name: "non-chat model", selection: profileModel("user/text-embedding-private"), want: protocol.ErrModelNotAvailable},
		{name: "unsupported effort", selection: profileEffort("max"), want: protocol.ErrEffortNotSupported},
		{name: "invalid collaboration", selection: profileCollaboration("party"), want: protocol.ErrInvalidProfile},
		{name: "invalid token", selection: profileToken("turbo"), want: protocol.ErrInvalidProfile},
		{name: "invalid approval", selection: profileApproval("sometimes"), want: protocol.ErrInvalidProfile},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := New().ResolveProfile(context.Background(), workspace, test.selection)
			if err == nil {
				t.Fatal("invalid profile was accepted")
			}
			if got, ok := catalog.ErrorCode(err); !ok || got != test.want {
				t.Fatalf("error code = %q, %v, want %q; err=%v", got, ok, test.want, err)
			}
		})
	}
}

func TestResolverReloadsConfigAndIsConcurrentSafe(t *testing.T) {
	_, workspace := profileFixture(t)
	resolver := New()
	first, err := resolver.ResolveProfile(context.Background(), workspace, protocol.ProfileSelection{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Model != "project/project-chat" {
		t.Fatalf("first model = %q", first.Model)
	}
	projectPath := filepath.Join(workspace, "reasonix.toml")
	writeProfileConfig(t, projectPath, `default_model = "project/project-alt"

[desktop]
default_tool_approval_mode = "yolo"

[[providers]]
name = "project"
kind = "openai"
base_url = "http://127.0.0.1:18081/v1"
models = ["project-chat", "project-alt"]
default = "project-chat"
supported_efforts = ["low", "high"]
default_effort = "low"
`)
	second, err := resolver.ResolveProfile(context.Background(), workspace, protocol.ProfileSelection{})
	if err != nil {
		t.Fatal(err)
	}
	if second.Model != "project/project-alt" || second.Effort != "low" {
		t.Fatalf("reloaded profile = %+v", second)
	}

	var wg sync.WaitGroup
	errorsSeen := make(chan error, 16)
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			profile, resolveErr := resolver.ResolveProfile(context.Background(), workspace, protocol.ProfileSelection{})
			if resolveErr != nil {
				errorsSeen <- resolveErr
				return
			}
			if profile != second {
				errorsSeen <- errors.New("concurrent resolution returned a different profile")
			}
		}()
	}
	wg.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		t.Fatal(err)
	}
}

func TestResolverRejectsCancellationAndRelativeWorkspaceBeforeConfigIO(t *testing.T) {
	resolver := New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := resolver.ResolveProfile(ctx, "relative", protocol.ProfileSelection{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled resolve error = %v", err)
	}
	if _, err := resolver.ResolveProfile(context.Background(), "relative", protocol.ProfileSelection{}); err == nil {
		t.Fatal("relative workspace was accepted")
	}
	var nilResolver *Resolver
	if _, err := nilResolver.ResolveProfile(context.Background(), filepath.Clean(t.TempDir()), protocol.ProfileSelection{}); err == nil {
		t.Fatal("nil resolver was accepted")
	}
}

func profileModel(value string) protocol.ProfileSelection {
	return protocol.ProfileSelection{Model: &value}
}

func profileEffort(value string) protocol.ProfileSelection {
	return protocol.ProfileSelection{Effort: &value}
}

func profileCollaboration(value string) protocol.ProfileSelection {
	mode := protocol.CollaborationMode(value)
	return protocol.ProfileSelection{CollaborationMode: &mode}
}

func profileToken(value string) protocol.ProfileSelection {
	mode := protocol.TokenMode(value)
	return protocol.ProfileSelection{TokenMode: &mode}
}

func profileApproval(value string) protocol.ProfileSelection {
	mode := protocol.ToolApprovalMode(value)
	return protocol.ProfileSelection{ToolApprovalMode: &mode}
}
