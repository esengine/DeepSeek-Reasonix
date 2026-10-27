package filelock

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRootedFileLockProcessHelper(t *testing.T) {
	for i, arg := range os.Args {
		if arg != "--rooted-file-lock-helper" {
			continue
		}
		if len(os.Args) != i+4 {
			os.Exit(2)
		}
		root, err := os.OpenRoot(os.Args[i+1])
		if err != nil {
			os.Exit(3)
		}
		release, err := TryAcquireRoot(root, os.Args[i+2])
		if os.Args[i+3] == "busy" {
			if !errors.Is(err, ErrHeld) {
				if release != nil {
					release()
				}
				os.Exit(4)
			}
			root.Close()
			fmt.Println("busy")
			os.Exit(0)
		}
		if err != nil {
			os.Exit(5)
		}
		fmt.Println("held")
		bufio.NewReader(os.Stdin).ReadString('\n')
		release()
		root.Close()
		os.Exit(0)
	}
}

func rootedProcessCommand(t *testing.T, dir, mode string) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)
	return exec.CommandContext(ctx, exe, "-test.run=^TestRootedFileLockProcessHelper$", "--", "--rooted-file-lock-helper", dir, "owner.lock", mode)
}

func TestRootedFileLockExcludesAnotherProcess(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "rooted", true: "legacy"}[legacy], func(t *testing.T) {
			dir, root := lockRoot(t)
			var release func()
			var err error
			if legacy {
				release, err = TryAcquire(filepath.Join(dir, "owner.lock"))
			} else {
				release, err = TryAcquireRoot(root, "owner.lock")
			}
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			out, err := rootedProcessCommand(t, dir, "busy").CombinedOutput()
			if err != nil || strings.TrimSpace(string(out)) != "busy" {
				t.Fatalf("process bypassed the held lock: %q %v", out, err)
			}
		})
	}
}

func TestRootedFileLockRecoversAfterProcessExit(t *testing.T) {
	for _, kill := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "killed"}[kill], func(t *testing.T) {
			dir, root := lockRoot(t)
			cmd := rootedProcessCommand(t, dir, "hold")
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			waited := false
			t.Cleanup(func() {
				stdin.Close()
				if !waited {
					cmd.Process.Kill()
					cmd.Wait()
				}
			})
			line, err := bufio.NewReader(stdout).ReadString('\n')
			if err != nil || line != "held\n" {
				t.Fatalf("child did not publish ownership: %q %v", line, err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if other, err := AcquireRoot(ctx, root, "owner.lock"); !errors.Is(err, context.Canceled) {
				if other != nil {
					other()
				}
				t.Fatalf("child ownership did not block admission: %v", err)
			}
			if other, err := TryAcquire(filepath.Join(dir, "owner.lock")); !errors.Is(err, ErrHeld) {
				if other != nil {
					other()
				}
				t.Fatalf("cancelled waiter released child ownership: %v", err)
			}
			if kill {
				if err := cmd.Process.Kill(); err != nil {
					t.Fatal(err)
				}
			} else if _, err := io.WriteString(stdin, "release\n"); err != nil {
				t.Fatal(err)
			}
			err = cmd.Wait()
			waited = true
			if !kill && err != nil {
				t.Fatalf("normal child exit: %v\n%s", err, stderr.String())
			}
			if kill && err == nil {
				t.Fatal("kill did not interrupt the holding process")
			}
			other, err := TryAcquireRoot(root, "owner.lock")
			if err != nil {
				t.Fatalf("OS ownership survived child termination: %v", err)
			}
			other()
		})
	}
}
