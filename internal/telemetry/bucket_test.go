package telemetry

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestSafeBucket(t *testing.T) {
	long := strings.Repeat("a", 100)
	for _, tc := range []struct {
		name     string
		value    string
		fallback string
		want     string
	}{
		{"trimmed and lowercased", "  v1.20.0  ", "other", "v1_20_0"},
		{"mixed case and symbols", "DeepSeek-R1 (beta)", "other", "deepseek_r1_beta"},
		{"spaces collapse to underscore", "a b c", "other", "a_b_c"},
		{"empty uses fallback", "", "other", "other"},
		{"whitespace only uses fallback", "   ", "other", "other"},
		{"only unsafe chars uses fallback", "---", "other", "other"},
		{"truncated to 96 chars", long, "other", strings.Repeat("a", 96)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := safeBucket(tc.value, tc.fallback); got != tc.want {
				t.Errorf("safeBucket(%q, %q) = %q, want %q", tc.value, tc.fallback, got, tc.want)
			}
		})
	}
}

func TestEnumBucket(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		want  string
	}{
		{"exact match", "run", "run"},
		{"case insensitive", "RUN", "run"},
		{"trimmed match", "  tui  ", "tui"},
		{"last allowed", "delivery", "delivery"},
		{"unknown", "bogus", "other"},
		{"empty", "", "other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := enumBucket(tc.value, "run", "tui", "delivery"); got != tc.want {
				t.Errorf("enumBucket(%q) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}

func TestPermissionBucket(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  string
	}{
		{"manual", "ask"},
		{"ask", "ask"},
		{"auto", "auto"},
		{"acceptEdits", "auto"},
		{"dontask", "dont_ask"},
		{"plan", "plan"},
		{"bypassPermissions", "yolo"},
		{"YOLO", "yolo"},
		{"", "other"},
		{"sandbox", "other"},
	} {
		if got := permissionBucket(tc.value); got != tc.want {
			t.Errorf("permissionBucket(%q) = %q, want %q", tc.value, got, tc.want)
		}
	}
}

func TestLanguageBucket(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  string
	}{
		{"zh", "zh"},
		{"zh-CN", "zh"},
		{"ZHTW", "zh"},
		{"en", "en"},
		{"en-US", "en"},
		{"auto", "auto"},
		{"AUTO", "auto"},
		{"", "auto"},
		{"fr", "other"},
	} {
		if got := languageBucket(tc.value); got != tc.want {
			t.Errorf("languageBucket(%q) = %q, want %q", tc.value, got, tc.want)
		}
	}
}

func TestFinishReasonBucket(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  string
	}{
		{"stop", "stop"},
		{"tool_calls", "tool_calls"},
		{"length", "length"},
		{"content_filter", "content_filter"},
		{"repetition_truncation", "repetition_truncation"},
		{"STOP", "stop"},
		{"", "unknown"},
		{"max_tokens", "other"},
	} {
		if got := finishReasonBucket(tc.value); got != tc.want {
			t.Errorf("finishReasonBucket(%q) = %q, want %q", tc.value, got, tc.want)
		}
	}
}

func TestCacheBucketBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name string
		hit  int
		miss int
		want string
	}{
		{"no tokens", 0, 0, "unknown"},
		{"zero total from negatives", -5, 5, "unknown"},
		{"zero percent", 0, 10, "0"},
		{"one percent", 1, 99, "1_24"},
		{"just under 25", 24, 76, "1_24"},
		{"exactly 25", 25, 75, "25_49"},
		{"just under 50", 49, 51, "25_49"},
		{"exactly 50", 50, 50, "50_74"},
		{"just under 75", 74, 26, "50_74"},
		{"exactly 75", 75, 25, "75_89"},
		{"just under 90", 89, 11, "75_89"},
		{"exactly 90", 90, 10, "90_100"},
		{"fully cached", 100, 0, "90_100"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := cacheBucket(tc.hit, tc.miss); got != tc.want {
				t.Errorf("cacheBucket(%d, %d) = %q, want %q", tc.hit, tc.miss, got, tc.want)
			}
		})
	}
}

func TestToolErrorBucket(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  string
	}{
		{"permission denied: mkdir", "permission"},
		{"blocked by policy", "permission"},
		{"access denied", "permission"},
		{"PERMISSION DENIED", "permission"},
		{"context deadline exceeded", "timeout"},
		{"cancelled by user", "cancelled"},
		{"not found: /tmp/x", "not_found"},
		{"no such file or directory", "not_found"},
		{"permission denied: timeout", "permission"},
		{"", "other"},
		{"connection reset", "other"},
	} {
		if got := toolErrorBucket(tc.value); got != tc.want {
			t.Errorf("toolErrorBucket(%q) = %q, want %q", tc.value, got, tc.want)
		}
	}
}

func TestProviderErrorBucketClassification(t *testing.T) {
	netErr := &net.DNSError{Err: "no such host", Name: "api.example.com"}
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"auth direct", &provider.AuthError{Provider: "deepseek", Status: 401}, "auth"},
		{"auth wrapped", fmt.Errorf("outer: %w", &provider.AuthError{Provider: "deepseek", Status: 401}), "auth"},
		{"api 429", &provider.APIError{Status: 429, Provider: "deepseek"}, "rate_limit"},
		{"api 500", &provider.APIError{Status: 500, Provider: "deepseek"}, "server"},
		{"api 503", &provider.APIError{Status: 503, Provider: "deepseek"}, "server"},
		{"api 400", &provider.APIError{Status: 400, Provider: "deepseek"}, "request"},
		{"api 422", &provider.APIError{Status: 422, Provider: "deepseek"}, "request"},
		{"api 200", &provider.APIError{Status: 200, Provider: "deepseek"}, "http"},
		{"api 300", &provider.APIError{Status: 300, Provider: "deepseek"}, "http"},
		{"api 429 wrapping deadline", fmt.Errorf("%w", &provider.APIError{Status: 429, Provider: "deepseek"}), "rate_limit"},
		{"deadline", context.DeadlineExceeded, "timeout"},
		{"deadline wrapped", fmt.Errorf("outer: %w", context.DeadlineExceeded), "timeout"},
		{"cancelled", context.Canceled, "cancelled"},
		{"network", netErr, "network"},
		{"stream interrupted", &provider.StreamInterruptedError{Err: errors.New("cut")}, "interrupted"},
		{"plain error", errors.New("boom"), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := providerErrorBucket(tc.err); got != tc.want {
				t.Errorf("providerErrorBucket(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

func TestLatencyBucketBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name string
		d    time.Duration
		want string
	}{
		{"zero", 0, "lt_1s"},
		{"just under 1s", time.Second - time.Nanosecond, "lt_1s"},
		{"exactly 1s", time.Second, "s_1_5"},
		{"just under 5s", 5*time.Second - time.Nanosecond, "s_1_5"},
		{"exactly 5s", 5 * time.Second, "s_5_15"},
		{"just under 15s", 15*time.Second - time.Nanosecond, "s_5_15"},
		{"exactly 15s", 15 * time.Second, "s_15_60"},
		{"just under 1m", time.Minute - time.Nanosecond, "s_15_60"},
		{"exactly 1m", time.Minute, "m_1_5"},
		{"just under 5m", 5*time.Minute - time.Nanosecond, "m_1_5"},
		{"exactly 5m", 5 * time.Minute, "m_5_15"},
		{"just under 15m", 15*time.Minute - time.Nanosecond, "m_5_15"},
		{"exactly 15m", 15 * time.Minute, "m_15_plus"},
		{"far beyond", time.Hour, "m_15_plus"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := latencyBucket(tc.d); got != tc.want {
				t.Errorf("latencyBucket(%v) = %q, want %q", tc.d, got, tc.want)
			}
		})
	}
}

func TestExitBucket(t *testing.T) {
	for _, tc := range []struct {
		name  string
		event event.Event
		want  string
	}{
		{"success", event.Event{}, "success"},
		{"cancelled flag", event.Event{Cancelled: true}, "cancelled"},
		{"cancelled context", event.Event{Err: context.Canceled}, "cancelled"},
		{"cancelled beats error", event.Event{Cancelled: true, Err: errors.New("boom")}, "cancelled"},
		{"recovery paused", event.Event{Outcome: event.TurnOutcomeRecoveryPaused}, "recovery_paused"},
		{"recovery paused beats error", event.Event{Outcome: event.TurnOutcomeRecoveryPaused, Err: errors.New("boom")}, "recovery_paused"},
		{"error", event.Event{Err: errors.New("boom")}, "error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := exitBucket(tc.event); got != tc.want {
				t.Errorf("exitBucket(%+v) = %q, want %q", tc.event, got, tc.want)
			}
		})
	}
}

func TestCountersFromAndAdd(t *testing.T) {
	counts := countersFrom([]Counter{
		{Signal: "turns", Bucket: "count", Count: 1},
		{Signal: "turns", Bucket: "count", Count: 2},
		{Signal: "cli_exit", Bucket: "success", Count: 1},
		{Signal: "cli_exit", Bucket: "success", Count: 0},
		{Signal: "ignored", Bucket: "negative", Count: -3},
	})
	if counts["turns\x00count"] != 3 {
		t.Errorf("aggregated turns/count = %d, want 3", counts["turns\x00count"])
	}
	if counts["cli_exit\x00success"] != 1 {
		t.Errorf("aggregated cli_exit/success = %d, want 1", counts["cli_exit\x00success"])
	}
	if _, ok := counts["ignored\x00negative"]; ok {
		t.Error("non-positive count should not be added")
	}

	m := map[string]int{}
	add(m, "a", "b", 0)
	add(m, "", "b", 1)
	add(m, "a", "", 1)
	add(m, "a", "b", -1)
	if len(m) != 0 {
		t.Errorf("invalid add calls populated map: %#v", m)
	}
	add(m, "a", "b", 2)
	add(m, "a", "b", 2)
	if m["a\x00b"] != 4 {
		t.Errorf("accumulated a/b = %d, want 4", m["a\x00b"])
	}
}

func TestValidCounter(t *testing.T) {
	valid96 := strings.Repeat("a", 96)
	tooLong := strings.Repeat("a", 97)
	for _, tc := range []struct {
		name string
		c    Counter
		want bool
	}{
		{"valid", Counter{Signal: "turns", Bucket: "count", Count: 1}, true},
		{"max count", Counter{Signal: "turns", Bucket: "count", Count: 1_000_000}, true},
		{"max bucket length", Counter{Signal: "turns", Bucket: valid96, Count: 1}, true},
		{"unknown signal", Counter{Signal: "nope", Bucket: "count", Count: 1}, false},
		{"zero count", Counter{Signal: "turns", Bucket: "count", Count: 0}, false},
		{"negative count", Counter{Signal: "turns", Bucket: "count", Count: -1}, false},
		{"count over cap", Counter{Signal: "turns", Bucket: "count", Count: 1_000_001}, false},
		{"empty bucket", Counter{Signal: "turns", Bucket: "", Count: 1}, false},
		{"bucket over 96", Counter{Signal: "turns", Bucket: tooLong, Count: 1}, false},
		{"unsafe bucket chars", Counter{Signal: "turns", Bucket: "Count-1", Count: 1}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := validCounter(tc.c); got != tc.want {
				t.Errorf("validCounter(%+v) = %v, want %v", tc.c, got, tc.want)
			}
		})
	}
}

func TestRemoveClaim(t *testing.T) {
	t.Run("existing file removed", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "a.json")
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		claimed := map[string]bool{path: true}
		removeClaim(path, claimed)
		if claimed[path] {
			t.Error("claimed entry not cleared after removal")
		}
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("file still present: %v", err)
		}
	})
	t.Run("missing file is idempotent", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing.json")
		claimed := map[string]bool{path: true}
		removeClaim(path, claimed)
		if claimed[path] {
			t.Error("claimed entry not cleared for missing file")
		}
	})
	t.Run("remove failure keeps claim", func(t *testing.T) {
		dir := t.TempDir()
		sub := filepath.Join(dir, "sub")
		if err := os.MkdirAll(filepath.Join(sub, "inner"), 0o700); err != nil {
			t.Fatal(err)
		}
		claimed := map[string]bool{sub: true}
		removeClaim(sub, claimed)
		if !claimed[sub] {
			t.Error("claim cleared despite failed removal")
		}
	})
}

func TestRandomHex(t *testing.T) {
	for _, n := range []int{0, 8, 16, 32} {
		got, err := randomHex(n)
		if err != nil {
			t.Fatalf("randomHex(%d): %v", n, err)
		}
		if len(got) != 2*n {
			t.Errorf("randomHex(%d) length = %d, want %d", n, len(got), 2*n)
		}
		if _, err := hex.DecodeString(got); err != nil {
			t.Errorf("randomHex(%d) = %q is not hex: %v", n, got, err)
		}
	}
	a, err := randomHex(16)
	if err != nil {
		t.Fatal(err)
	}
	b, err := randomHex(16)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("randomHex produced identical values across calls")
	}
}
