package isolation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/filelock"
)

func TestMain(m *testing.M) {
	name := strings.ToLower(filepath.Base(os.Args[0]))
	if (name == "git" || name == "git.exe") && os.Getenv("REASONIX_TEST_OPERATION_GIT_REAL") != "" {
		os.Exit(operationGitShim())
	}
	os.Exit(m.Run())
}

func operationGitShim() int {
	pause := os.Getenv("REASONIX_TEST_OPERATION_GIT_PAUSE")
	for i, arg := range os.Args[1:] {
		match := pause == "restore" && arg == "restore"
		if pause == "remove" && arg == "worktree" && i+2 < len(os.Args) {
			match = os.Args[i+2] == "remove"
		}
		if !match {
			continue
		}
		conn, err := net.DialTimeout("tcp", os.Getenv("REASONIX_TEST_OPERATION_GIT_ADDR"), 15*time.Second)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 120
		}
		conn.SetDeadline(time.Now().Add(time.Minute))
		_, err = io.ReadFull(conn, make([]byte, 1))
		conn.Close()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 121
		}
		break
	}
	cmd := exec.Command(os.Getenv("REASONIX_TEST_OPERATION_GIT_REAL"), os.Args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		var exited *exec.ExitError
		if errors.As(err, &exited) {
			return exited.ExitCode()
		}
		fmt.Fprintln(os.Stderr, err)
		return 122
	}
	return 0
}

func TestActualResultOperationHoldsOwnershipThroughWriteAndCleanup(t *testing.T) {
	for _, stage := range []string{"restore", "remove"} {
		t.Run(stage, func(t *testing.T) {
			s, e := finishedOperationResult(t)
			realGit, err := exec.LookPath("git")
			if err != nil {
				t.Fatal(err)
			}
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			binary, err := os.ReadFile(exe)
			if err != nil {
				t.Fatal(err)
			}
			shim := t.TempDir()
			name := "git"
			if runtime.GOOS == "windows" {
				name += ".exe"
			}
			if err := os.WriteFile(filepath.Join(shim, name), binary, 0o700); err != nil {
				t.Fatal(err)
			}
			listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			if err := listener.SetDeadline(time.Now().Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			t.Setenv("REASONIX_TEST_OPERATION_GIT_REAL", realGit)
			t.Setenv("REASONIX_TEST_OPERATION_GIT_PAUSE", stage)
			t.Setenv("REASONIX_TEST_OPERATION_GIT_ADDR", listener.Addr().String())
			t.Setenv("PATH", shim+string(os.PathListSeparator)+os.Getenv("PATH"))
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				var err error
				if stage == "restore" {
					_, err = s.Apply(ctx, e.ID)
				} else {
					_, err = s.Discard(ctx, e.ID)
				}
				done <- err
			}()
			conn, err := listener.Accept()
			if err != nil {
				cancel()
				<-done
				t.Fatalf("operation did not reach its real Git boundary: %v", err)
			}
			defer conn.Close()
			t.Setenv("REASONIX_TEST_OPERATION_GIT_PAUSE", "")
			peer := NewStore(s.managedRoot, s.workspace, s.repo)
			if extra, err := peer.acquireResultOperation(t.Context(), e.ID, true); !errors.Is(err, filelock.ErrHeld) {
				if extra != nil {
					extra()
				}
				t.Errorf("actual %s call released its lease before the Git boundary: %v", stage, err)
			}
			request, stop := context.WithCancel(t.Context())
			stop()
			if _, err := peer.Discard(request, e.ID); !errors.Is(err, context.Canceled) {
				t.Errorf("discard entered during active %s: %v", stage, err)
			}
			if _, err := os.Stat(e.Cand.WorktreeRoot); err != nil {
				t.Errorf("peer deleted the active operation's candidate: %v", err)
			}
			if _, err := conn.Write([]byte{1}); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatalf("actual operation failed after boundary release: %v", err)
			}
			want := "one\n"
			if stage == "restore" {
				want = "candidate\n"
			}
			if got := read(t, s.workspace, "a.txt"); got != want {
				t.Fatalf("operation output = %q, want %q", got, want)
			}
		})
	}
}
