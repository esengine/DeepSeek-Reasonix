//go:build windows

package desktopinstance

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestNativeStatusPipeValidatesIdentityAndProtocol(t *testing.T) {
	p, err := openProcess(windows.GetCurrentProcessId(), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()
	for _, tc := range []struct {
		name   string
		schema int
		pid    uint32
		valid  bool
	}{
		{"valid", 1, p.pid, true}, {"wrong-pid", 1, p.pid + 1, false}, {"future-schema", 2, p.pid, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			name, _ := windows.UTF16PtrFromString(fmt.Sprintf(`\\.\pipe\reasonix-shell-v1-%d`, p.pid))
			pipe, err := windows.CreateNamedPipe(name, windows.PIPE_ACCESS_OUTBOUND, windows.PIPE_TYPE_BYTE, 1, StatusLimit+1, 0, 2000, nil)
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				defer windows.CloseHandle(pipe)
				err := windows.ConnectNamedPipe(pipe, nil)
				if err != nil && !errors.Is(err, windows.ERROR_PIPE_CONNECTED) {
					done <- err
					return
				}
				data, _ := json.Marshal(Status{SchemaVersion: tc.schema, Product: "com.reasonix.desktop", PID: tc.pid, Version: "v1.38.7", Generation: "test", HomeKey: ProfileKey(t.TempDir()), Lifecycle: "failed", Service: "exited"})
				var n uint32
				err = windows.WriteFile(pipe, append(data, '\n'), &n, nil)
				if err == nil {
					err = windows.FlushFileBuffers(pipe)
				}
				done <- err
			}()
			_, err = readStatus(p)
			if (err == nil) != tc.valid {
				t.Errorf("valid=%v error=%v", tc.valid, err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNativeProcessIdentityAndPIDReuseFence(t *testing.T) {
	if os.Getenv("REASONIX_PROCESS_TEST_CHILD") == "1" {
		time.Sleep(time.Minute)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestNativeProcessIdentityAndPIDReuseFence$")
	cmd.Env = append(os.Environ(), "REASONIX_PROCESS_TEST_CHILD=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Wait()
	defer cmd.Process.Kill()
	p, err := openProcess(uint32(cmd.Process.Pid), uint32(os.Getpid()))
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()
	if !p.alive() {
		t.Fatal("child already dead")
	}
	saved := p.created
	p.created.LowDateTime++
	if err := p.terminate(); err == nil {
		t.Fatal("accepted changed creation time")
	}
	if !p.alive() {
		t.Fatal("killed identity that did not match")
	}
	p.created = saved
	if err := p.terminate(); err != nil {
		t.Fatal(err)
	}
	if !waitProcesses([]*process{p}, time.Second*10) {
		t.Fatal("handle did not signal exit")
	}
}

func TestNativeCoordinationMutexOwnsCorrectThread(t *testing.T) {
	root := t.TempDir()
	unlock, err := lockInstall(root)
	if err != nil {
		t.Fatal(err)
	}
	waiting := make(chan struct{})
	acquired := make(chan error, 1)
	go func() {
		close(waiting)
		release, err := lockInstall(root)
		if err == nil {
			release()
		}
		acquired <- err
	}()
	<-waiting
	select {
	case err := <-acquired:
		unlock()
		t.Fatalf("mutex did not serialize callers: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	unlock()
	select {
	case err := <-acquired:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("mutex was not released on owning thread")
	}
}

func TestNativeInspectCurrentUser(t *testing.T) {
	p, err := openProcess(windows.GetCurrentProcessId(), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()
	if p.image == "" || !p.alive() {
		t.Fatal("missing executable identity")
	}
}

// Optional local diagnostic: never runs against the user's default profile.
func TestNativeExplicitFixtureInspection(t *testing.T) {
	root, home := os.Getenv("REASONIX_TEST_INSPECT_ROOT"), os.Getenv("REASONIX_TEST_INSPECT_HOME")
	if root == "" || home == "" {
		t.Skip("explicit isolated installed fixture required")
	}
	root, profile, err := preparePaths(root, home)
	if err != nil {
		t.Fatal(err)
	}
	list, err := inspect(root, profile, false)
	if err != nil {
		t.Fatal(err)
	}
	defer closeProcesses(list)
	if len(list) == 0 {
		t.Fatal("isolated fixture shell was not identified")
	}
	for _, p := range list {
		t.Logf("pid=%d legacyProfile=%q status=%+v", p.pid, p.legacyProfile, p.status)
	}
}
