package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMissingReasoningWarnStateLegacyV2FallbackUsesShortestAdaptiveBackoff(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, missingReasoningWarnStateFilename)
	fingerprint := warningFingerprint("legacy-fixed-fallback")
	openedAt := missingReasoningTestNow()
	doc := fmt.Sprintf(`{"version":2,"incidents":[{"fingerprint":"%s","warnedAtUnixMs":%d,"lastMissingAtUnixMs":%d,"lastMissingAtUnixNano":%d,"fallbackAtUnixNano":%d}]}`,
		fingerprint, openedAt.UnixMilli(), openedAt.UnixMilli(), openedAt.UnixNano(), openedAt.UnixNano())
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}

	s := newMissingReasoningWarnState(dir)
	if got := s.claimRecoveryModeAt(fingerprint, openedAt.Add(missingReasoningFallbackBackoffs[0]-time.Nanosecond)).Mode; got != missingReasoningRecoveryFallback {
		t.Fatalf("legacy mode before adaptive boundary = %v, want fallback", got)
	}
	decision := s.claimRecoveryModeAt(fingerprint, openedAt.Add(missingReasoningFallbackBackoffs[0]))
	if decision.Mode != missingReasoningRecoveryProbe {
		t.Fatalf("legacy mode at adaptive boundary = %+v, want probe", decision)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"version":2`) || !strings.Contains(string(b), `"fallbackLevel":1`) ||
		!strings.Contains(string(b), `"probeClaimedAtUnixNano"`) {
		t.Fatalf("legacy fallback did not migrate in-place within v2: %s", b)
	}
}
