package gitcmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func storageFixture(t *testing.T, text string) Repo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git unavailable")
	}
	dir := testenv.TempDir(t)
	if out, err := Command(t.Context(), dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("setup: %v %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "file.txt"}, {"commit", "-q", "-m", "fixture"}} {
		if out, err := CommandWithConfig(t.Context(), dir, []string{"user.name=Fixture", "user.email=fixture@example.invalid"}, args...).CombinedOutput(); err != nil {
			t.Fatalf("setup: %v %s", err, out)
		}
	}
	repo, err := Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestHostStorageEnvironment(t *testing.T) {
	for _, key := range []string{"GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, filepath.Join(t.TempDir(), "ambient"))
			for _, entry := range Env() {
				name, _, _ := strings.Cut(entry, "=")
				if strings.EqualFold(name, key) {
					t.Fatalf("ambient %s survived", key)
				}
			}
		})
	}
}

func TestHostObjectStorage(t *testing.T) {
	t.Run("writes_stay_in_repository", func(t *testing.T) {
		repo := storageFixture(t, "source\n")
		outside := testenv.TempDir(t)
		t.Setenv("GIT_OBJECT_DIRECTORY", outside)
		cmd := repo.Command(t.Context(), "hash-object", "-w", "--stdin")
		cmd.Stdin = strings.NewReader("new host object\n")
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		oid := strings.TrimSpace(string(out))
		if _, err := os.Stat(filepath.Join(repo.CommonDir, "objects", oid[:2], oid[2:])); err != nil {
			t.Fatalf("object not in resolved repository: %v", err)
		}
		if _, err := os.Stat(filepath.Join(outside, oid[:2], oid[2:])); !os.IsNotExist(err) {
			t.Fatalf("ambient object directory was written: %v", err)
		}
	})
	t.Run("ambient_alternate_not_read", func(t *testing.T) {
		repo := storageFixture(t, "source\n")
		foreign := storageFixture(t, "foreign object\n")
		out, err := foreign.Command(t.Context(), "rev-parse", "HEAD:file.txt").Output()
		if err != nil {
			t.Fatal(err)
		}
		oid := strings.TrimSpace(string(out))
		t.Setenv("GIT_ALTERNATE_OBJECT_DIRECTORIES", filepath.Join(foreign.CommonDir, "objects"))
		if err := repo.Command(t.Context(), "cat-file", "-e", oid).Run(); err == nil {
			t.Fatal("host read an object only ambient alternates supplied")
		}
	})
}

func TestHostExplicitStorageControls(t *testing.T) {
	t.Run("private_index", func(t *testing.T) {
		repo := storageFixture(t, "before\n")
		index := filepath.Join(repo.GitDir, "index")
		before, err := os.ReadFile(index)
		if err != nil {
			t.Fatal(err)
		}
		ambient := filepath.Join(t.TempDir(), "ambient-index")
		if err := os.WriteFile(ambient, []byte("not an index"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("GIT_INDEX_FILE", ambient)
		if err := os.WriteFile(filepath.Join(repo.WorkTree, "file.txt"), []byte("after\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		private := filepath.Join(t.TempDir(), "private-index")
		if err := repo.StageAll(t.Context(), "GIT_INDEX_FILE="+private); err != nil {
			t.Fatal(err)
		}
		cmd := repo.Command(t.Context(), "show", ":file.txt")
		cmd.Env = append(cmd.Env, "GIT_INDEX_FILE="+private)
		out, err := cmd.Output()
		if err != nil || string(out) != "after\n" {
			t.Fatalf("explicit private index = %q %v", out, err)
		}
		after, err := os.ReadFile(index)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("private staging touched the user's real index")
		}
		untouched, err := os.ReadFile(ambient)
		if err != nil || string(untouched) != "not an index" {
			t.Fatal("explicit staging changed ambient index")
		}
	})
	t.Run("explicit_object_directory", func(t *testing.T) {
		repo := storageFixture(t, "source\n")
		outside := testenv.TempDir(t)
		t.Setenv("GIT_OBJECT_DIRECTORY", filepath.Join(t.TempDir(), "missing"))
		cmd := repo.Command(t.Context(), "hash-object", "-w", "--stdin")
		cmd.Env = append(cmd.Env, "GIT_OBJECT_DIRECTORY="+outside)
		cmd.Stdin = strings.NewReader("explicit host object\n")
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		oid := strings.TrimSpace(string(out))
		if _, err := os.Stat(filepath.Join(outside, oid[:2], oid[2:])); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("repository_declared_alternate", func(t *testing.T) {
		repo := storageFixture(t, "source\n")
		foreign := storageFixture(t, "declared alternate\n")
		out, err := foreign.Command(t.Context(), "rev-parse", "HEAD:file.txt").Output()
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(repo.CommonDir, "objects", "info", "alternates")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(filepath.ToSlash(filepath.Join(foreign.CommonDir, "objects"))+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := repo.Command(t.Context(), "cat-file", "-e", strings.TrimSpace(string(out))).Run(); err != nil {
			t.Fatalf("declared alternate disabled: %v", err)
		}
	})
}

func TestDetachedStorageEnvironment(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git unavailable")
	}
	for _, inherited := range []bool{false, true} {
		t.Run(map[bool]string{false: "explicit_env", true: "inherited_env"}[inherited], func(t *testing.T) {
			cmd := exec.Command("git", "--version")
			if inherited {
				t.Setenv("GIT_INDEX_FILE", "ambient-index")
				t.Setenv("GIT_OBJECT_DIRECTORY", "ambient-objects")
				t.Setenv("GIT_ALTERNATE_OBJECT_DIRECTORIES", "ambient-alternates")
			} else {
				cmd.Env = append(os.Environ(), "GIT_INDEX_FILE=ambient-index", "GIT_OBJECT_DIRECTORY=ambient-objects", "GIT_ALTERNATE_OBJECT_DIRECTORIES=ambient-alternates")
			}
			cleanup, err := Detached(cmd)
			if err != nil {
				t.Fatal(err)
			}
			defer cleanup()
			for _, entry := range cmd.Env {
				name, _, _ := strings.Cut(entry, "=")
				switch strings.ToUpper(name) {
				case "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES":
					t.Fatalf("detached command retained %s", name)
				}
			}
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("detached git: %v %s", err, out)
			}
		})
	}
}
