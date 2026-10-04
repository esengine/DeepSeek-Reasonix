package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"reasonix/internal/platform/remote"
	"reasonix/internal/platform/remote/sftpfs"
)

// shellConn runs the capture through a real sh with HOME confined to a temp
// directory, so rc files cannot reach anything outside it.
type shellConn struct{ home, shell string }

func (c *shellConn) Exec(ctx context.Context, cmd string) (remote.ExecResult, error) {
	x := exec.CommandContext(ctx, "/bin/sh", "-c", cmd)
	x.Env = []string{"HOME=" + c.home, "ZDOTDIR=" + c.home, "SHELL=" + c.shell, "PATH=/usr/bin:/bin"}
	x.Dir = c.home
	var out, errb bytes.Buffer
	x.Stdout, x.Stderr = &out, &errb
	err := x.Run()
	res := remote.ExecResult{Stdout: out.Bytes(), Stderr: errb.Bytes()}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		res.ExitCode, err = ee.ExitCode(), nil
	}
	return res, err
}

func (c *shellConn) SFTP() (*sftpfs.FS, error) { return nil, errors.New("no sftp") }

func TestCaptureScriptThroughRealShells(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shells only")
	}
	shells := []struct{ bin, rc string }{
		{"/bin/bash", ".bash_profile"}, {"/bin/zsh", ".zshrc"}, {"/usr/bin/zsh", ".zshrc"},
	}
	rc := `echo "banner"; printf 'fake:path=/fake\n'
export PATH='/x/bin':"$PATH"
export PATH="$PATH:"'/a b/$(touch "$HOME/PWN1")/` + "`touch \"$HOME/PWN2\"`" + `/;touch $HOME/PWN3'
false
`
	for _, sh := range shells {
		if _, err := os.Stat(sh.bin); err != nil {
			continue
		}
		t.Run(sh.bin+sh.rc, func(t *testing.T) {
			home := t.TempDir()
			if err := os.WriteFile(filepath.Join(home, sh.rc), []byte(rc), 0o644); err != nil {
				t.Fatal(err)
			}
			env := captureLoginEnv(context.Background(), &shellConn{home: home, shell: sh.bin})
			if env.Outcome != EnvCaptured || len(env.Path) == 0 || env.Path[0] != "/x/bin" {
				t.Fatalf("env = %+v", env)
			}
			conn := &shellConn{home: home, shell: sh.bin}
			if _, err := withLoginEnv(env, conn).Exec(context.Background(), "command -v ls"); err != nil {
				t.Fatal(err)
			}
			for _, f := range []string{"PWN1", "PWN2", "PWN3"} {
				if _, err := os.Stat(filepath.Join(home, f)); err == nil {
					t.Fatalf("%s created: hostile PATH text was executed", f)
				}
			}
			for _, e := range env.Path {
				if e == "/fake" {
					t.Fatal("forged line leaked into PATH")
				}
			}
		})
	}
}
