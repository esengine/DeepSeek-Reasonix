package computer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestCUAInstalledApplicationRemainsDiscoverableAndLaunchesExplicitly(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("REASONIX_CUA_INSTALLED_APP", exe)
	log := filepath.Join(testenv.TempDir(t), "requests.jsonl")
	t.Setenv("REASONIX_CUA_REQUEST_LOG", log)
	s := fakeSession(t)
	apps, err := s.Apps(t.Context())
	if err != nil || len(apps) != 1 || apps[0].Bundle != exe || apps[0].PID != 0 {
		t.Fatalf("installed application disappeared: %+v %v", apps, err)
	}
	if _, err := s.Snapshot(t.Context(), exe); CodeOf(err) != CodeNoWindow {
		t.Fatalf("installed state was not explained: %v", err)
	}
	if _, err := s.Snapshot(t.Context(), filepath.Base(exe)); CodeOf(err) != CodeNoWindow {
		t.Fatalf("hidden canonical identity looked uninstalled: %v", err)
	}
	body, _ := os.ReadFile(log)
	if strings.Contains(string(body), `"name":"launch_app"`) {
		t.Fatal("read launched an application")
	}
	res, err := s.Act(t.Context(), exe, []Step{{Action: "launch"}})
	if err != nil || res.Done != 1 || res.App.Bundle != filepath.Base(exe) {
		t.Fatalf("launch: %+v %v", res, err)
	}
	body, _ = os.ReadFile(log)
	if strings.Count(string(body), `"name":"launch_app"`) != 1 || !strings.Contains(string(body), `"path":`) {
		t.Fatalf("missing exact native launch: %s", body)
	}
	apps, err = s.Apps(t.Context())
	if err != nil || len(apps) != 1 || apps[0].PID <= 0 || apps[0].Bundle != filepath.Base(exe) {
		t.Fatalf("restored: %+v %v", apps, err)
	}
}

func TestCUALaunchMustNotCombineWithInput(t *testing.T) {
	exe, _ := os.Executable()
	t.Setenv("REASONIX_CUA_INSTALLED_APP", exe)
	s := fakeSession(t)
	for _, steps := range [][]Step{{{Action: "launch"}, {Action: "click", Ref: "old"}}, {{Action: "launch", Text: "--payload"}}} {
		if _, err := s.Act(t.Context(), exe, steps); CodeOf(err) != CodeBadStep {
			t.Fatalf("unsafe launch accepted: %v", err)
		}
	}
}

func TestCUAWindowsLaunchRejectsArgumentsAndForbiddenPackageFamilies(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, launcher := range []string{exe + " --flag", "relative.exe", `\\server\share\app.exe`} {
		if _, err := applicationLaunch(nativeApplication{Bundle: exe, LaunchPath: launcher}); CodeOf(err) != CodeUnsupported {
			t.Fatalf("launcher accepted %q: %v", launcher, err)
		}
	}
	for _, identity := range []string{"windows.immersivecontrolpanel_cw5n1h2txyewy!Application", "Microsoft.SecHealthUI_8wekyb3d8bbwe!App"} {
		if _, err := applicationLaunch(nativeApplication{Bundle: identity}); CodeOf(err) != CodeAppRefused {
			t.Fatalf("forbidden AUMID accepted: %q %v", identity, err)
		}
	}
	spec, err := applicationLaunch(nativeApplication{Bundle: exe, LaunchPath: `"` + exe + `"`})
	if err != nil || spec.args["path"] != spec.executable {
		t.Fatalf("quoted path launch: %+v %v", spec, err)
	}
	spec, err = applicationLaunch(nativeApplication{Bundle: "Example.Package!App"})
	if CodeOf(err) != CodeUnsupported {
		t.Fatalf("unattested package launch: %+v %v", spec, err)
	}
	for _, file := range []string{`\\server\share\program.exe`, `\\?\C:\program.exe`, exe + ".dll", "relative.exe"} {
		if err := attestNativeLaunchBinary(file); CodeOf(err) != CodeUnsupported {
			t.Fatalf("resolved path accepted: %q %v", file, err)
		}
	}
}
