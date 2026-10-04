package installsource

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestRevisionExternalWatchHelper(t *testing.T) {
	root := os.Getenv("REASONIX_REVISION_WATCH_ROOT")
	if root == "" {
		t.Skip("subprocess helper")
	}
	holdPluginDirectoryWatch(t, root)
	fmt.Fprintln(os.Stdout, "watch ready")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}

func TestRevisionUpdateWithSubprocessDirectoryWatch(t *testing.T) {
	tool, source := revisionPlugin(t)
	act := plannedPluginCopy(t, tool, source)
	if err := tool.applyInstallPluginPackage(t.Context(), request{}, &act); err != nil {
		t.Fatal(err)
	}
	old := act.Target
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestRevisionExternalWatchHelper$")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	cmd.Env = append(os.Environ(), "REASONIX_REVISION_WATCH_ROOT="+filepath.Join(old, "skills"))
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
	defer func() { _ = stdin.Close(); _ = cmd.Wait() }()
	ready, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || ready != "watch ready\n" {
		t.Fatalf("watch readiness %q, %v", ready, err)
	}
	if err := os.Rename(old, old+".probe"); !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		if err == nil {
			_ = os.Rename(old+".probe", old)
		}
		t.Fatalf("subprocess watch must deny ancestor rename: %v", err)
	}
	writeFile(t, filepath.Join(source, "skills", "neutral", "SKILL.md"), "---\nname: neutral\ndescription: Neutral fixture\n---\nNEW")
	act = plannedPluginCopy(t, tool, source)
	if err := tool.applyInstallPluginPackage(t.Context(), request{Replace: true}, &act); err != nil {
		t.Fatalf("watched update: %v", err)
	}
	if act.Target == old {
		t.Fatal("update renamed watched tree")
	}
	if body, err := os.ReadFile(filepath.Join(old, "skills", "neutral", "SKILL.md")); err != nil || string(body) != "---\nname: neutral\ndescription: Neutral fixture\n---\nAPPROVED" {
		t.Fatalf("watched tree lost: %q, %v", body, err)
	}
}
