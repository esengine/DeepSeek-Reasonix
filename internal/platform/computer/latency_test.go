package computer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestCUATargetObservationDoesNotWalkUnrelatedWindows(t *testing.T) {
	for _, capture := range []bool{false, true} {
		t.Run(map[bool]string{false: "tree", true: "capture"}[capture], func(t *testing.T) {
			t.Setenv("REASONIX_CUA_EXTRA_APPS", "12")
			log := filepath.Join(testenv.TempDir(t), "requests.jsonl")
			t.Setenv("REASONIX_CUA_REQUEST_LOG", log)
			s := fakeSession(t)
			var err error
			if capture {
				_, _, err = s.Screenshot(t.Context(), "com.example.Notes")
			} else {
				_, err = s.Snapshot(t.Context(), "com.example.Notes")
			}
			if err != nil {
				t.Fatal(err)
			}
			body, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			windows := 0
			for line := range strings.SplitSeq(strings.TrimSpace(string(body)), "\n") {
				var req struct {
					Name string         `json:"name"`
					Args map[string]any `json:"arguments"`
				}
				if err := json.Unmarshal([]byte(line), &req); err != nil {
					t.Fatal(err)
				}
				if req.Name == "list_windows" {
					windows++
					if req.Args["pid"] != float64(s.state.app.PID) {
						t.Errorf("window query escaped target pid: %+v", req.Args)
					}
				}
				if req.Name == "get_window_state" {
					if req.Args["include_screenshot"] != capture || req.Args["include_accessibility_tree"] != !capture {
						t.Errorf("unrequested observation work: %+v", req.Args)
					}
				}
			}
			if windows != 1 {
				t.Errorf("queried %d window lists for one target", windows)
			}
		})
	}
}

func TestCUAApplicationDiscoveryUsesOneWindowEnumeration(t *testing.T) {
	t.Setenv("REASONIX_CUA_EXTRA_APPS", "12")
	log := filepath.Join(testenv.TempDir(t), "requests.jsonl")
	t.Setenv("REASONIX_CUA_REQUEST_LOG", log)
	s := fakeSession(t)
	apps, err := s.Apps(t.Context())
	if err != nil || len(apps) != 13 {
		t.Fatalf("apps=%d err=%v", len(apps), err)
	}
	for _, app := range apps {
		if len(app.Windows) != 1 {
			t.Fatalf("lost pid-bound windows: %+v", app)
		}
	}
	body, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(body), `"name":"list_windows"`); n != 1 {
		t.Fatalf("window enumerations=%d", n)
	}
}
