package config

import (
	"github.com/BurntSushi/toml"
	"slices"
	"strings"
	"testing"
)

func TestCollaborationDefaultsToLegacyForExistingConfig(t *testing.T) {
	if mode, err := Default().CollaborationMode(); err != nil || mode != "legacy" {
		t.Fatalf("%s: %v", mode, err)
	}
	for _, value := range []string{"auto", "shared", "isolated"} {
		cfg, _ := loadScoped(t, "[agent]\ncollaboration = \""+value+"\"\n", "")
		if mode, err := cfg.CollaborationMode(); err != nil || mode != value {
			t.Fatalf("%s: %v", mode, err)
		}
		if !strings.Contains(RenderTOMLForScope(cfg, RenderScopeUser), "collaboration = \""+value+"\"") {
			t.Fatal("mode was not preserved")
		}
	}
}

func TestAutoIntegrationDefaultsAndUserAuthorityRoundTrip(t *testing.T) {
	if !Default().AutoIntegrationEnabled() {
		t.Fatal("automatic integration is not enabled by default")
	}
	for _, enabled := range []bool{true, false} {
		user, project := "false", "true"
		if enabled {
			user, project = project, user
		}
		cfg, _ := loadScoped(t, "[agent]\nauto_integrate = "+user+"\n", "[agent]\nauto_integrate = "+project+"\n")
		if cfg.AutoIntegrationEnabled() != enabled {
			t.Fatal("project changed integration authority")
		}
		clone, err := cfg.CloneResolved()
		if err != nil {
			t.Fatal(err)
		}
		if clone.AutoIntegrationEnabled() != enabled {
			t.Fatal("clone lost choice")
		}
		*clone.Agent.AutoIntegrate = !enabled
		if cfg.AutoIntegrationEnabled() != enabled {
			t.Fatal("clone aliases saved pointer")
		}
		for _, scope := range []RenderScope{RenderScopeUser, RenderScopeFull} {
			back := Default()
			if _, err := toml.Decode(RenderTOMLForScope(cfg, scope), back); err != nil || back.AutoIntegrationEnabled() != enabled {
				t.Fatalf("round trip: %v", err)
			}
		}
		if strings.Contains(RenderTOMLForScope(cfg, RenderScopeProject), "auto_integrate") {
			t.Fatal("project render published user authority")
		}
	}
}

func TestIsolationCopiesRemainUserOwnedAndRoundTrip(t *testing.T) {
	cfg, _ := loadScoped(t, "[agent]\nisolation_copy_paths = [\"deps\"]\n", "[agent]\nisolation_copy_paths = [\"secrets\"]\n")
	if !slices.Equal(cfg.Agent.IsolationCopyPaths, []string{"deps"}) {
		t.Fatalf("project expanded copy authority: %v", cfg.Agent.IsolationCopyPaths)
	}
	for _, scope := range []RenderScope{RenderScopeFull, RenderScopeUser} {
		back := Default()
		if _, err := toml.Decode(RenderTOMLForScope(cfg, scope), back); err != nil || !slices.Equal(back.Agent.IsolationCopyPaths, []string{"deps"}) {
			t.Fatalf("declared copies lost in %s render: %v", scope, err)
		}
	}
	if strings.Contains(RenderTOMLForScope(cfg, RenderScopeProject), "isolation_copy_paths") {
		t.Fatal("project render published a user's copy authority")
	}
	clone, err := cfg.CloneResolved()
	if err != nil {
		t.Fatal(err)
	}
	clone.Agent.IsolationCopyPaths[0] = "other"
	if cfg.Agent.IsolationCopyPaths[0] != "deps" {
		t.Fatal("isolated config aliases source copy declaration")
	}
}

func TestProjectCannotChangeCollaborationMode(t *testing.T) {
	cfg, _ := loadScoped(t, "[agent]\ncollaboration = \"shared\"\n", "[agent]\ncollaboration = \"auto\"\n")
	if mode, err := cfg.CollaborationMode(); err != nil || mode != "shared" {
		t.Fatalf("%s: %v", mode, err)
	}
}

func TestVerificationRuntimeRemainsHostOwnedAcrossProjectAndRender(t *testing.T) {
	cfg, _ := loadScoped(t, "[agent]\nverification_runtime = \"/trusted/image\"\n", "[agent]\nverification_runtime = \"/repo/fake\"\n")
	if cfg.Agent.VerificationRuntime != "/trusted/image" {
		t.Fatal("project selected verification runtime")
	}
	for _, scope := range []RenderScope{RenderScopeUser, RenderScopeFull} {
		back := Default()
		if _, err := toml.Decode(RenderTOMLForScope(cfg, scope), back); err != nil || back.Agent.VerificationRuntime != "/trusted/image" {
			t.Fatalf("runtime lost: %v", err)
		}
	}
	if strings.Contains(RenderTOMLForScope(cfg, RenderScopeProject), "verification_runtime") {
		t.Fatal("project render widened execution authority")
	}
}
