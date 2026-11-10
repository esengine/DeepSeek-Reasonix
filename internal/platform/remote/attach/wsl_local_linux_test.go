//go:build linux

package attach

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/platform/remote"
	"reasonix/internal/platform/wsl"
)

// localSession answers wsl.exe's --exec by running the program here with HOME
// pointing at a directory the test owns, so the staging scripts and the session
// shell meet a real GNU userland.
func localSession(home string) wsl.Command {
	return func(ctx context.Context, stdin io.Reader, args ...string) (wsl.Result, error) {
		for i, a := range args {
			if a == "--exec" {
				args = args[i+1:]
				break
			}
		}
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.Env = append(os.Environ(), "HOME="+home)
		var out, errb bytes.Buffer
		cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, &out, &errb
		err := cmd.Run()
		res := wsl.Result{Stdout: out.Bytes(), Stderr: errb.Bytes()}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			res.ExitCode, err = ee.ExitCode(), nil
		}
		return res, err
	}
}

func localLink(t *testing.T, home string) Link {
	t.Helper()
	if err := exec.Command("stat", "-c", "%a", "/").Run(); err != nil {
		t.Skip("needs GNU stat")
	}
	m := &wslMachine{t: t}
	l, err := newWSLLink(t.Context(), config.RemoteHostEntry{Name: "dev", Kind: "wsl", Distro: "Ubuntu"}, m.list, localSession(home))
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func detach(l Link) func(context.Context, string) (remote.ExecResult, error) {
	return l.(interface {
		ExecDetached(context.Context, string) (remote.ExecResult, error)
	}).ExecDetached
}

func TestALaunchRunsInARealSessionAndKeepsItsOwnStatus(t *testing.T) {
	l := localLink(t, testenv.TempDir(t))
	if err := l.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	res, err := detach(l)(t.Context(), "echo out; echo err >&2; exit 44")
	if err != nil || string(res.Stdout) != "out\n" || string(res.Stderr) != "err\n" || res.ExitCode != 44 {
		t.Fatalf("ExecDetached = %+v, %v", res, err)
	}
}

// mkdir -p succeeds on a directory someone else prepared, so the staging
// directory is checked after it is made and never trusted for existing.
func TestAScratchDirectoryOthersCouldUseIsRefused(t *testing.T) {
	for name, prepare := range map[string]func(t *testing.T, dir string){
		"group and world accessible": func(t *testing.T, dir string) {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(dir, 0o755); err != nil {
				t.Fatal(err)
			}
		},
		"world writable": func(t *testing.T, dir string) {
			if err := os.MkdirAll(dir, 0o777); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(dir, 0o777); err != nil {
				t.Fatal(err)
			}
		},
		"a symlink to elsewhere": func(t *testing.T, dir string) {
			if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
				t.Fatal(err)
			}
			elsewhere := testenv.TempDir(t)
			if err := os.Chmod(elsewhere, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(elsewhere, dir); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			home := testenv.TempDir(t)
			prepare(t, filepath.Join(home, ".reasonix", "link"))
			l := localLink(t, home)
			if err := l.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			defer l.Close()
			if _, err := detach(l)(t.Context(), "echo hi"); !errors.Is(err, ErrUnsafeScratch) {
				t.Fatalf("err = %v, want ErrUnsafeScratch", err)
			}
			entries, _ := os.ReadDir(filepath.Join(home, ".reasonix", "link"))
			for _, e := range entries {
				t.Errorf("a command was staged in the unsafe directory: %s", e.Name())
			}
		})
	}
}
