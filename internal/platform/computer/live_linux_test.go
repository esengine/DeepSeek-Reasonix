//go:build linux

package computer

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

func TestLiveLinuxOperatesAnOwnedGTKApplication(t *testing.T) {
	s := liveSession(t)
	dir := testenv.TempDir(t)
	exe, log := filepath.Join(dir, "reasonix-cua-target"), filepath.Join(dir, "events.log")
	flags, err := exec.Command("pkg-config", "--cflags", "--libs", "gtk+-3.0").Output()
	if err != nil {
		t.Fatal(err)
	}
	args := append([]string{"testdata/target_linux.c", "-o", exe}, strings.Fields(string(flags))...)
	if out, err := exec.Command("gcc", args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v: %s", err, out)
	}
	target := exec.Command(exe, log)
	if err := target.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = target.Process.Kill(); _ = target.Wait() })
	waitLog(t, log, "ready")
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	var bundle string
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		apps, err := s.Apps(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, app := range apps {
			if app.PID == int32(target.Process.Pid) && len(app.Windows) == 1 {
				bundle = app.Bundle
			}
		}
		if bundle != "" {
			break
		}
	}
	if bundle == "" {
		t.Fatal("driver did not discover the owned GTK window")
	}
	snap, err := s.Snapshot(ctx, bundle)
	if err != nil {
		t.Fatal(err)
	}
	field := lineRef(t, snap.Lines, `"Probe field"`)
	result, err := s.Act(ctx, bundle, []Step{{Action: "set_value", Ref: field, Text: "from CUA 李雷"}})
	if err != nil || result.Done != 1 {
		t.Fatalf("value: %+v: %v", result, err)
	}
	waitLog(t, log, "text from CUA 李雷")
	snap, err = s.Snapshot(ctx, bundle)
	if err != nil {
		t.Fatal(err)
	}
	button := lineRef(t, snap.Lines, `"Probe button"`)
	result, err = s.Act(ctx, bundle, []Step{{Action: "click", Ref: button}})
	if err != nil && CodeOf(err) != CodeEffectUnknown {
		t.Fatalf("click: %+v: %v", result, err)
	}
	if result.Done != 1 {
		t.Fatal("delivered click missing from result")
	}
	waitLog(t, log, "button pressed 1")
	shot, app, err := s.Screenshot(ctx, bundle)
	if err != nil || !strings.HasPrefix(shot, "data:image/") || app.PID != int32(target.Process.Pid) {
		t.Fatalf("capture: %+v %v", app, err)
	}
}
