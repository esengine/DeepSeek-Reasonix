//go:build linux

package wslconn

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/platform/remote/execfs/fstest"
	"reasonix/internal/platform/wsl"
)

// localCommand answers wsl.exe's --exec by running the program here, so the
// scripts meet a real GNU userland without a distribution.
func localCommand(t *testing.T) wsl.Command {
	t.Helper()
	return func(ctx context.Context, stdin io.Reader, args ...string) (wsl.Result, error) {
		for i, a := range args {
			if a == "--exec" {
				args = args[i+1:]
				break
			}
		}
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
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

func TestFileLayerAgainstALocalPOSIXShell(t *testing.T) {
	if err := exec.Command("stat", "-c", "%s", "/").Run(); err != nil {
		t.Skip("needs GNU stat")
	}
	c := &Conn{distro: "local", run: localCommand(t)}
	fstest.RunScenario(t, c.Volume(), testenv.TempDir(t))
}

func TestExecRunsUnderTheShellInTheDistribution(t *testing.T) {
	c := &Conn{distro: "local", run: localCommand(t)}
	res, err := c.Exec(context.Background(), "printf '%s' \"$((6*7))\"; echo err >&2; exit 3")
	if err != nil || string(res.Stdout) != "42" || strings.TrimSpace(string(res.Stderr)) != "err" || res.ExitCode != 3 {
		t.Fatalf("Exec = %+v, %v", res, err)
	}
}
