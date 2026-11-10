package computer

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func installedFixture(t *testing.T) (string, string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	identity := exe
	if runtime.GOOS == "darwin" {
		identity = "com.example.Notes"
	}
	if runtime.GOOS == "linux" {
		identity = "com.example.Notes.desktop"
	}
	t.Setenv("REASONIX_CUA_INSTALLED_APP", exe)
	t.Setenv("REASONIX_CUA_INSTALLED_ID", identity)
	log := filepath.Join(testenv.TempDir(t), "requests.jsonl")
	t.Setenv("REASONIX_CUA_REQUEST_LOG", log)
	return identity, log
}

func TestCUALaunchUsesFreshUniqueCatalogueAndInvalidatesReferences(t *testing.T) {
	identity, _ := installedFixture(t)
	t.Setenv("REASONIX_CUA_INSTALLED_EXTRA_RUNNING", "1")
	s := fakeSession(t)
	snap, err := s.Snapshot(t.Context(), "com.example.Other")
	if err != nil {
		t.Fatal(err)
	}
	ref := lineRef(t, snap.Lines, `"Body"`)
	res, err := s.Act(t.Context(), identity, []Step{{Action: "launch"}})
	if err != nil || res.Done != 1 || res.Steps[0].Effect.Class != EffectUnverifiable {
		t.Fatalf("launch %+v %v", res, err)
	}
	if _, err := s.Act(t.Context(), res.App.Bundle, []Step{{Action: "click", Ref: ref}}); CodeOf(err) != CodeStaleRef {
		t.Fatalf("launch reused a previous observation: %v", err)
	}
}

func TestCUALaunchRejectsAmbiguityAndUnknownTargetsBeforeNativeDispatch(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		t.Run(map[bool]string{false: "unknown", true: "ambiguous"}[duplicate], func(t *testing.T) {
			identity, log := installedFixture(t)
			if duplicate {
				t.Setenv("REASONIX_CUA_DUPLICATE_INSTALLED", "1")
			} else {
				identity = "unlisted.application"
			}
			s := fakeSession(t)
			if _, err := s.Act(t.Context(), identity, []Step{{Action: "launch"}}); CodeOf(err) != CodeNoApp {
				t.Fatalf("unexpected launch: %v", err)
			}
			body, _ := os.ReadFile(log)
			if strings.Contains(string(body), `"name":"launch_app"`) {
				t.Fatal("unresolved launch reached driver")
			}
		})
	}
}

func TestCUALaunchNeverBorrowsAnotherProcessAfterNativeSuccess(t *testing.T) {
	for _, pid := range []int{0, os.Getpid()} {
		t.Run(map[bool]string{true: "missing", false: "different"}[pid == 0], func(t *testing.T) {
			identity, log := installedFixture(t)
			t.Setenv("REASONIX_CUA_OVERRIDE_TOOL", "launch_app")
			response := `{"structuredContent":{"pid":0}}`
			if pid > 0 {
				response = `{"structuredContent":{"pid":` + strconv.Itoa(pid) + `,"bundle_id":"` + identity + `"}}`
				if runtime.GOOS == "windows" {
					response = `{"structuredContent":{"pid":` + strconv.Itoa(pid) + `}}`
				}
			}
			t.Setenv("REASONIX_CUA_OVERRIDE_RESULT", response)
			s := fakeSession(t)
			res, err := s.Act(t.Context(), identity, []Step{{Action: "launch"}})
			if CodeOf(err) != CodeEffectUnknown || res.Done != 1 || res.App.PID != 0 {
				t.Fatalf("borrowed target: %+v %v", res, err)
			}
			body, _ := os.ReadFile(log)
			if strings.Contains(string(body), `"name":"list_windows"`) || strings.Contains(string(body), `"name":"get_window_state"`) {
				t.Fatal("read another process after launch mismatch")
			}
		})
	}
}
