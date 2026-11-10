package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"reasonix/internal/base/testenv"
)

func rootsFixture(t *testing.T) (cfg *Config, home, userHome, primary string, extraFolder func() string) {
	t.Helper()
	extraFolder = tempAside(t)
	home, userHome, primary = testenv.TempDir(t), testenv.TempDir(t), testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", userHome)
	t.Setenv("USERPROFILE", userHome)
	for _, dir := range []string{".ssh", ".gnupg", ".aws"} {
		if err := os.MkdirAll(filepath.Join(userHome, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(userHome, ".ssh", "id_ed25519"), []byte("k"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userHome, ".aws", "credentials"), []byte("k"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := RootsForHome(home).LoadForRootReadOnly(primary)
	if err != nil {
		t.Fatal(err)
	}
	return cfg, home, userHome, primary, extraFolder
}

func refusalOf(t *testing.T, cfg *Config, primary, dir string) WorkspaceRootRefusal {
	t.Helper()
	_, err := cfg.VetExtraRoot(primary, dir, nil)
	var refusal *WorkspaceRootError
	if !errors.As(err, &refusal) {
		t.Fatalf("VetExtraRoot(%q) = %v, want a refusal", dir, err)
	}
	return refusal.Code
}

func TestVetExtraRootRefusesWhatHoldsKeysOrTheSystem(t *testing.T) {
	cfg, home, userHome, primary, extraFolder := rootsFixture(t)
	elsewhere := extraFolder()
	viaLink := filepath.Join(elsewhere, "innocent")
	linked := os.Symlink(filepath.Join(userHome, ".ssh"), viaLink) == nil

	for name, tc := range map[string]struct {
		dir  string
		want WorkspaceRootRefusal
	}{
		"ssh dir":          {filepath.Join(userHome, ".ssh"), RootProtected},
		"gnupg dir":        {filepath.Join(userHome, ".gnupg"), RootProtected},
		"cloud credential": {filepath.Join(userHome, ".aws"), RootProtected},
		"user home":        {userHome, RootTooBroad},
		"state home":       {home, RootProtected},
		"parent of home":   {filepath.Dir(userHome), RootTooBroad},
		"drive root":       {filepath.VolumeName(os.TempDir()) + string(filepath.Separator), RootTooBroad},
		"primary":          {primary, RootCoveredAlready},
		"relative":         {"sub", RootNotDirectory},
	} {
		t.Run(name, func(t *testing.T) {
			if got := refusalOf(t, cfg, primary, tc.dir); got != tc.want {
				t.Fatalf("refusal = %s, want %s", got, tc.want)
			}
		})
	}
	if linked {
		t.Run("symlink to the ssh dir", func(t *testing.T) {
			if got := refusalOf(t, cfg, primary, viaLink); got != RootProtected {
				t.Fatalf("refusal = %s, want %s", got, RootProtected)
			}
		})
	}
	if runtime.GOOS != "windows" {
		t.Run("system dir", func(t *testing.T) {
			if got := refusalOf(t, cfg, primary, "/etc"); got != RootNotOwned {
				t.Fatalf("refusal = %s", got)
			}
		})
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		t.Run("case-folded ssh dir", func(t *testing.T) {
			if got := refusalOf(t, cfg, primary, filepath.Join(userHome, ".SSH")); got != RootProtected {
				t.Fatalf("refusal = %s", got)
			}
		})
	}
	if _, err := cfg.VetExtraRoot(primary, elsewhere, nil); err != nil {
		t.Fatalf("an ordinary folder was refused: %v", err)
	}
}

func TestVetExtraRootReadsTheConfiguredForbidList(t *testing.T) {
	cfg, _, _, primary, extraFolder := rootsFixture(t)
	secret, ordinary := extraFolder(), extraFolder()
	cfg.Sandbox.ForbidRead = append(cfg.Sandbox.ForbidRead, secret)
	if got := refusalOf(t, cfg, primary, secret); got != RootProtected {
		t.Fatalf("a forbid_read folder was offered: %s", got)
	}
	inside := filepath.Join(ordinary, "vault")
	if err := os.Mkdir(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg.Sandbox.ForbidRead = append(cfg.Sandbox.ForbidRead, inside)
	if got := refusalOf(t, cfg, primary, ordinary); got != RootProtected {
		t.Fatalf("a folder containing a forbidden one was offered: %s", got)
	}
}

func TestGrantedRootIsVettedAgainAtEveryLoad(t *testing.T) {
	cfg, home, userHome, primary, extraFolder := rootsFixture(t)
	extra := filepath.Join(extraFolder(), "shared")
	if err := os.Mkdir(extra, 0o755); err != nil {
		t.Fatal(err)
	}
	real, err := cfg.VetExtraRoot(primary, extra, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewProjectGrantStore(home).Update(primary, func(g ProjectGrant) (ProjectGrant, error) {
		g.AllowWrite = append(g.AllowWrite, real)
		return g, nil
	}); err != nil {
		t.Fatal(err)
	}
	load := func() *Config {
		c, err := RootsForHome(home).LoadForRootReadOnly(primary)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	if got := load().ExtraRoots(); !slices.Equal(got, []string{real}) {
		t.Fatalf("granted roots = %v, want %v", got, []string{real})
	}

	// The folder is replaced by a link to the ssh directory after the grant.
	if err := os.RemoveAll(real); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(userHome, ".ssh"), real); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	swapped := load()
	if got := swapped.ExtraRoots(); len(got) != 0 {
		t.Fatalf("a root swapped for a link to ~/.ssh stayed granted: %v", got)
	}
	if slices.Contains(swapped.WriteRootsForRoot(primary), filepath.Join(userHome, ".ssh")) {
		t.Fatal("the swapped target reached the write roots")
	}
	ignored := swapped.IgnoredProjectSettings()
	if len(ignored) == 0 || ignored[len(ignored)-1].Reason != ProjectRootRefused {
		t.Fatalf("the refusal was not reported: %+v", ignored)
	}
}

func TestVetExtraRootKeepsClearOfEveryOtherWritableRoot(t *testing.T) {
	cfg, _, _, primary, extraFolder := rootsFixture(t)
	parent := extraFolder()
	child := filepath.Join(parent, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.VetExtraRoot(primary, parent, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.VetExtraRoot(primary, parent, []string{parent}); err != nil {
		t.Fatalf("granting the same root again must stay idempotent: %v", err)
	}
	_, err := cfg.VetExtraRoot(primary, child, []string{parent})
	var refusal *WorkspaceRootError
	if !errors.As(err, &refusal) || refusal.Code != RootOverlapsWrite {
		t.Fatalf("a root inside another extra root: %v", err)
	}
	_, err = cfg.VetExtraRoot(primary, parent, []string{child})
	if !errors.As(err, &refusal) || refusal.Code != RootOverlapsWrite {
		t.Fatalf("a root holding another extra root: %v", err)
	}
	underTemp := filepath.Join(os.TempDir(), "granted-under-temp")
	if err := os.MkdirAll(underTemp, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = cfg.VetExtraRoot(primary, underTemp, nil)
	if !errors.As(err, &refusal) || refusal.Code != RootOverlapsWrite {
		t.Fatalf("a folder under the temp directory, which any sandboxed command can rewrite: %v", err)
	}
}

func TestExtraRootsAreWriteRootsButNotGuardExemptions(t *testing.T) {
	cfg, home, _, primary, extraFolder := rootsFixture(t)
	extra := extraFolder()
	real, err := cfg.VetExtraRoot(primary, extra, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewProjectGrantStore(home).Update(primary, func(g ProjectGrant) (ProjectGrant, error) {
		g.AllowWrite = append(g.AllowWrite, real)
		return g, nil
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err := RootsForHome(home).LoadForRootReadOnly(primary)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(loaded.WriteRootsForRoot(primary), real) {
		t.Fatal("a granted root is not a write root")
	}
	if slices.Contains(loaded.AllowWriteRoots(), real) {
		t.Fatal("a granted root became a raw-access exemption, which would lift the guard on Reasonix's own state")
	}
}

func TestVetExtraRootRefusesAMovedStateRoot(t *testing.T) {
	cfg, _, _, primary, extraFolder := rootsFixture(t)
	moved := extraFolder()
	t.Setenv("REASONIX_STATE_HOME", moved)
	cfg, err := RootsForHome(cfg.Roots().Dir(RootHome)).LoadForRootReadOnly(primary)
	if err != nil {
		t.Fatal(err)
	}
	if got := refusalOf(t, cfg, primary, moved); got != RootProtected {
		t.Fatalf("a relocated state root was offered as a workspace folder: %s", got)
	}
}

func TestVetExtraRootComparesFileIdentityNotSpelling(t *testing.T) {
	cfg, _, userHome, primary, _ := rootsFixture(t)
	spellings := map[string]string{}
	if runtime.GOOS == "darwin" {
		realHome, _ := filepath.EvalSymlinks(userHome)
		firm := filepath.Join("/System/Volumes/Data", realHome)
		if _, err := os.Stat(firm); err == nil {
			spellings["firmlink ssh dir"] = filepath.Join(firm, ".ssh")
			spellings["firmlink home"] = firm
		}
	}
	if runtime.GOOS == "darwin" && len(spellings) == 0 {
		t.Fatal("the firmlink spelling of the home directory does not exist here, so nothing was tested")
	}
	for name, dir := range spellings {
		t.Run(name, func(t *testing.T) {
			if got := refusalOf(t, cfg, primary, dir); got != RootProtected && got != RootTooBroad {
				t.Fatalf("%s was offered: %s", dir, got)
			}
			if code, refused := cfg.Roots().FolderGrantRefusal(dir); !refused {
				t.Fatalf("trust and allow_write accepted %s (%s)", dir, code)
			}
		})
	}
	// A relative link to the same directory through a second hop is also the same file.
	link := filepath.Join(testenv.TempDir(t), "hop")
	if os.Symlink(filepath.Join(userHome, ".ssh"), link) == nil {
		if got := refusalOf(t, cfg, primary, link); got != RootProtected {
			t.Fatalf("a link to ~/.ssh: %s", got)
		}
	}
}

// The skills folder is judged apart from the other protected data: it is the
// one whose refusal has a supported way forward to name.
func TestSkillStoreRefusalIsItsOwnIdentity(t *testing.T) {
	cfg, home, _, primary, _ := rootsFixture(t)
	skills := filepath.Join(home, UserSkillsDirName)
	if err := os.MkdirAll(filepath.Join(skills, "my-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, dir := range map[string]string{"the folder": skills, "one skill in it": filepath.Join(skills, "my-skill")} {
		if got := refusalOf(t, cfg, primary, dir); got != RootSkillStore {
			t.Fatalf("%s: refusal = %s, want %s", name, got, RootSkillStore)
		}
	}
	if err := os.MkdirAll(filepath.Join(home, "memory"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := refusalOf(t, cfg, primary, filepath.Join(home, "memory")); got != RootProtected {
		t.Fatalf("a sibling data folder = %s, want it still plainly protected", got)
	}
}
