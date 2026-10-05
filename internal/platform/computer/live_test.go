//go:build darwin || windows

package computer

import (
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// liveSession drives the real helper named by REASONIX_LIVE_COMPUTER, which
// needs a desktop (and on macOS two permissions), so ordinary runs skip. One
// session for the run, which is how a host holds one: on macOS, while one
// helper process has captured, a capture in another never returns.
func liveSession(t *testing.T) *Session {
	t.Helper()
	path := os.Getenv("REASONIX_LIVE_COMPUTER")
	if path == "" || (runtime.GOOS != "darwin" && runtime.GOOS != "windows") {
		t.Skip("set REASONIX_LIVE_COMPUTER to a built computer-use helper to run live tests")
	}
	liveOnce.Do(func() { live = NewSession(NewHelper(path)) })
	// A locked screen hides every application's windows from accessibility, so
	// what follows would fail as a missing element rather than as the machine
	// being put away.
	var status struct {
		ScreenLocked bool `json:"screen_locked"`
	}
	if err := live.helper.Call(t.Context(), "status", nil, &status); err != nil {
		t.Fatalf("the helper did not answer: %v", err)
	}
	if status.ScreenLocked {
		t.Skip("the screen is locked; unlock it to run live computer tests")
	}
	return live
}

var (
	liveOnce sync.Once
	live     *Session
)

func waitLog(t *testing.T, path, needle string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if body, _ := os.ReadFile(path); strings.Contains(string(body), needle) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	body, _ := os.ReadFile(path)
	t.Fatalf("the target never logged %q:\n%s", needle, body)
}

func lineRef(t *testing.T, lines []string, needle string) string {
	t.Helper()
	for _, l := range lines {
		if strings.Contains(l, needle) {
			if m := regexp.MustCompile(`\[(a\d+)\]`).FindStringSubmatch(l); m != nil {
				return m[1]
			}
		}
	}
	t.Fatalf("no ref on a line containing %q:\n%s", needle, strings.Join(lines, "\n"))
	return ""
}
