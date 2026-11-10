package scheduler

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/state/workspacelist"
)

func TestARequestedFolderIsOnlyEverComparedWithTheTrustedList(t *testing.T) {
	root := testenv.TempDir(t)
	inner := filepath.Join(root, "app")
	other := filepath.Join(root, "other")
	for _, d := range []string{inner, other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	list := []string{inner}
	for name, tc := range map[string]struct {
		in   string
		want bool
	}{
		"the project itself":        {inner, true},
		"with a trailing separator": {inner + string(filepath.Separator), true},
		"through a dot segment":     {filepath.Join(inner, ".") + string(filepath.Separator) + ".", true},
		"its parent":                {root, false},
		"a sibling":                 {other, false},
		"a path through ..":         {filepath.Join(inner, "..", "other"), false},
		"a child of the project":    {filepath.Join(inner, "sub"), false},
		"a relative path":           {"app", false},
		"a relative dot":            {".", false},
		"empty":                     {"", false},
		"blank":                     {"   ", false},
		"not there":                 {filepath.Join(root, "missing"), false},
	} {
		got, ok := Match(list, tc.in)
		if ok != tc.want || (ok && got != inner) {
			t.Errorf("%s: Match(%q) = %q, %v; want %v", name, tc.in, got, ok, tc.want)
		}
	}
}

func TestADraftNamesATrustedFolderOrIsRefusedAsUnknown(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	inner := testenv.TempDir(t)
	d := Draft{Prompt: "p", EverySec: 3600, Workspace: filepath.Join(inner, "..")}
	if _, err := d.Request([]string{inner}); !errors.Is(err, ErrDraftFolderUnknown) {
		t.Fatalf("a parent directory = %v, want ErrDraftFolderUnknown", err)
	}
	d.Workspace = inner
	if _, err := d.Request(nil); !errors.Is(err, ErrDraftFolderUnknown) {
		t.Fatalf("no trusted project at all = %v, want ErrDraftFolderUnknown", err)
	}
}

func TestTheTrustedFoldersAreTheRememberedProjectsThePersonTrusted(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	remembered, declined, undecided, current := testenv.TempDir(t), testenv.TempDir(t), testenv.TempDir(t), testenv.TempDir(t)
	list := filepath.Join(config.MemoryUserDir(), workspacelist.FileName)
	if err := workspacelist.Update(t.Context(), list, false, func(l *workspacelist.List) error {
		l.Paths = append(l.Paths, remembered, declined, undecided)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	grants := config.NewProjectGrantStore(config.RootsForHome("").Home())
	for dir, trust := range map[string]config.WorkspaceTrust{remembered: config.WorkspaceTrusted, declined: config.WorkspaceTrustDeclined} {
		if err := grants.SetTrust(dir, trust); err != nil {
			t.Fatal(err)
		}
	}
	if got := Folders(current); len(got) != 1 || got[0] != remembered {
		t.Fatalf("Folders = %v, want only the remembered project that was trusted (an untrusted current one is not one)", got)
	}
	if err := grants.SetTrust(current, config.WorkspaceTrusted); err != nil {
		t.Fatal(err)
	}
	if got := Folders(current); len(got) != 2 || got[1] != current {
		t.Fatalf("Folders = %v, want the trusted current project added", got)
	}
	if got := Folders(remembered); len(got) != 1 {
		t.Fatalf("a project listed and current appears twice: %v", got)
	}
}

func TestATrustedProjectThatIsGoneOrIsNotAFolderIsNotListed(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	gone, file := testenv.TempDir(t), filepath.Join(testenv.TempDir(t), "afile")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	list := filepath.Join(config.MemoryUserDir(), workspacelist.FileName)
	if err := workspacelist.Update(t.Context(), list, false, func(l *workspacelist.List) error {
		l.Paths = append(l.Paths, gone, file)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	grants := config.NewProjectGrantStore(config.RootsForHome("").Home())
	for _, p := range []string{gone, file} {
		if err := grants.SetTrust(p, config.WorkspaceTrusted); err != nil {
			t.Fatal(err)
		}
	}
	if got := Folders(""); !slices.Contains(got, gone) {
		t.Fatalf("a present trusted project is missing: %v", got)
	}
	if got := Folders(""); slices.Contains(got, file) {
		t.Fatalf("a file was listed as a project: %v", got)
	}
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	if got := Folders(""); slices.Contains(got, gone) {
		t.Fatalf("a deleted project is still listed: %v", got)
	}
}
