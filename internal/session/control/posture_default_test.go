package control

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/contract/config"
)

// A defaulted session follows the folder's trust; a named one keeps its posture.
func TestDefaultPostureFollowsTrustUntilNamed(t *testing.T) {
	home, ws := t.TempDir(), t.TempDir()
	c := New(Options{WorkspaceRoot: ws, Posture: PostureEvidence{WritesConfined: true, Home: home}})
	c.ApplyDefaultPosture()
	if got := c.Posture(); !got.Defaulted || got.Trust != config.WorkspaceTrustUndecided || !got.Trustable || !got.WritesConfined {
		t.Fatalf("posture after the default = %+v", got)
	}
	if err := c.DecideWorkspaceTrust(config.WorkspaceTrusted); err != nil {
		t.Fatal(err)
	}
	if got := c.ToolApprovalMode(); got != ToolApprovalAuto || !c.Posture().Defaulted {
		t.Fatalf("trusting a confined folder left %q (defaulted %v)", got, c.Posture().Defaulted)
	}
	if err := c.DecideWorkspaceTrust(config.WorkspaceTrustDeclined); err != nil {
		t.Fatal(err)
	}
	if got := c.ToolApprovalMode(); got != ToolApprovalAsk {
		t.Fatalf("declining the folder left %q", got)
	}
	c.SetToolApprovalMode(ToolApprovalAuto)
	if c.Posture().Defaulted {
		t.Fatal("a named posture still reads as the default")
	}
	if err := c.DecideWorkspaceTrust(config.WorkspaceTrustDeclined); err != nil {
		t.Fatal(err)
	}
	if got := c.ToolApprovalMode(); got != ToolApprovalAuto {
		t.Fatalf("a trust answer moved a named posture to %q", got)
	}
}

// Without a confining sandbox trust records but the default still asks.
func TestDefaultPostureAsksWithoutConfinement(t *testing.T) {
	c := New(Options{WorkspaceRoot: t.TempDir(), Posture: PostureEvidence{Home: t.TempDir()}})
	c.ApplyDefaultPosture()
	if err := c.DecideWorkspaceTrust(config.WorkspaceTrusted); err != nil {
		t.Fatal(err)
	}
	if got := c.ToolApprovalMode(); got != ToolApprovalAsk {
		t.Fatalf("an unconfined, trusted folder opens in %q", got)
	}
}

// A trust answer re-derives only a posture that is still the default: a switch
// that lands first is named and stays, whatever order the two race in.
func TestTrustAnswerNeverOverridesAConcurrentSwitch(t *testing.T) {
	for i := range 200 {
		c := New(Options{WorkspaceRoot: t.TempDir(), Posture: PostureEvidence{WritesConfined: true, Home: t.TempDir()}})
		c.ApplyDefaultPosture()
		done := make(chan struct{})
		go func() {
			defer close(done)
			_ = c.DecideWorkspaceTrust(config.WorkspaceTrusted)
		}()
		c.SetToolApprovalMode(ToolApprovalAsk)
		<-done
		if c.Posture().Defaulted {
			if got := c.ToolApprovalMode(); got != ToolApprovalAuto {
				t.Fatalf("still defaulted but in %q", got)
			}
			continue
		}
		if got := c.ToolApprovalMode(); got != ToolApprovalAsk {
			t.Fatalf("round %d: a named ask became %q", i, got)
		}
	}
}

func TestDecideWorkspaceTrustRefusesHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	c := New(Options{WorkspaceRoot: home, Posture: PostureEvidence{WritesConfined: true, Home: t.TempDir()}})
	if err := c.DecideWorkspaceTrust(config.WorkspaceTrusted); !errors.Is(err, ErrUntrustableFolder) {
		t.Fatalf("trusting home = %v", err)
	}
	if err := c.DecideWorkspaceTrust(config.WorkspaceTrustDeclined); err != nil {
		t.Fatalf("declining home = %v", err)
	}
}

// Nothing that holds a home directory, and nothing of Reasonix's own, is
// trusted as a whole — spelled directly or reached through a symlink.
func TestTrustableFolderRefusesWhatHoldsHomeOrReasonixState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	rxHome := filepath.Join(t.TempDir(), "rx")
	if err := os.MkdirAll(rxHome, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REASONIX_HOME", rxHome)
	project := filepath.Join(home, "code", "app")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	links := t.TempDir()
	linkRoot := filepath.Join(links, "to-root")
	linkHome := filepath.Join(links, "to-home")
	canLink := os.Symlink(string(filepath.Separator), linkRoot) == nil && os.Symlink(home, linkHome) == nil
	for _, tc := range []struct {
		name string
		dir  string
		want bool
	}{
		{"a project", project, true},
		{"home", home, false},
		{"above home", filepath.Dir(home), false},
		{"filesystem root", string(filepath.Separator), false},
		{"reasonix home", rxHome, false},
		{"inside reasonix home", filepath.Join(rxHome, "skills"), false},
		{"holding reasonix home", filepath.Dir(rxHome), false},
		{"link to root", linkRoot, !canLink},
		{"link to home", linkHome, !canLink},
	} {
		if tc.dir == linkRoot || tc.dir == linkHome {
			if !canLink {
				continue
			}
		}
		if got := TrustableFolder(tc.dir); got != tc.want {
			t.Errorf("%s: TrustableFolder(%q) = %v, want %v", tc.name, tc.dir, got, tc.want)
		}
	}
}
