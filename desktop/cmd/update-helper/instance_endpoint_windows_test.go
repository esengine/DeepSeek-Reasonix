//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"reasonix/desktop/internal/instanceidentity"
)

// This worker uses the real message-only window and mutex contract from Wails.
func TestInstanceEndpointWorker(t *testing.T) {
	id := os.Getenv("REASONIX_TEST_ENDPOINT")
	if id == "" {
		t.Skip("subprocess fixture")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	name := "wails-app-" + id
	mutexName, _ := windows.UTF16PtrFromString(name + "sim")
	mutex, err := windows.CreateMutex(nil, false, mutexName)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(mutex)
	class, _ := windows.UTF16PtrFromString(name + "-sic")
	title, _ := windows.UTF16PtrFromString(name + "-siw")
	def := handoffUser32.NewProc("DefWindowProcW")
	callback := syscall.NewCallback(func(hwnd uintptr, msg uint32, w, l uintptr) uintptr {
		r, _, _ := def.Call(hwnd, uintptr(msg), w, l)
		return r
	})
	wc := struct {
		Size, Style                        uint32
		Proc                               uintptr
		ClassExtra, WindowExtra            int32
		Instance, Icon, Cursor, Background uintptr
		Menu, Class                        *uint16
		SmallIcon                          uintptr
	}{Proc: callback, Class: class}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if r, _, err := handoffUser32.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		t.Fatal(err)
	}
	hwnd, _, err := handoffUser32.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)), 0, 0, 0, 0, 0, ^uintptr(2), 0, 0, 0)
	if hwnd == 0 {
		t.Fatal(err)
	}
	defer handoffUser32.NewProc("DestroyWindow").Call(hwnd)
	marker := os.Getenv("REASONIX_TEST_MARKER")
	if err := os.WriteFile(marker, []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker + ".stop"); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("fixture timed out")
}

func TestNativeEndpointIsolationAndHandoff(t *testing.T) {
	root, homeA, homeB := t.TempDir(), t.TempDir(), t.TempDir()
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "reasonix-desktop.exe")
	old := filepath.Join(root, "versions", "old", "reasonix-desktop.exe")
	if err := os.MkdirAll(filepath.Dir(old), 0700); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{target, old} {
		if err := os.WriteFile(p, payload, 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("REASONIX_HOME", homeA)
	t.Setenv(instanceidentity.UpdateEnvironmentKey, instanceidentity.ForHome(homeA))
	start := func(path, home string) func() {
		marker := filepath.Join(t.TempDir(), "ready")
		cmd := exec.Command(path, "-test.run=^TestInstanceEndpointWorker$")
		cmd.Env = append(os.Environ(), "REASONIX_TEST_ENDPOINT="+instanceidentity.ForHome(home), "REASONIX_TEST_MARKER="+marker)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		stopped := false
		stop := func() {
			if stopped {
				return
			}
			stopped = true
			_ = os.WriteFile(marker+".stop", nil, 0600)
			if err := cmd.Wait(); err != nil {
				t.Errorf("worker: %v", err)
			}
		}
		t.Cleanup(stop)
		deadline := time.Now().Add(10 * time.Second)
		for {
			if _, err := os.Stat(marker); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("worker did not register endpoint")
			}
			time.Sleep(20 * time.Millisecond)
		}
		return stop
	}
	start(old, homeB)
	if err := verifyDesktopHandoff(root, false); err != nil {
		t.Fatalf("independent old home blocked upgrade: %v", err)
	}
	stopA := start(old, homeA)
	if err := verifyDesktopHandoff(root, false); err == nil {
		t.Fatal("same-home old owner was accepted")
	}
	if image, err := desktopEndpointImage(instanceidentity.ForHome(homeB)); err != nil || image == "" {
		t.Fatalf("independent owner lost: %s %v", image, err)
	}
	stopA()
	start(target, homeA)
	if err := verifyDesktopHandoff(root, true); err != nil {
		t.Fatal(err)
	}
	if image, err := desktopEndpointImage(instanceidentity.ForHome(homeB)); err != nil || image == "" {
		t.Fatal(fmt.Errorf("independent owner did not survive: %w", err))
	}
}
