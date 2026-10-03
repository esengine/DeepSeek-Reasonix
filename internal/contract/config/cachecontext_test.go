package config

import (
	"strings"
	"testing"

	"reasonix/internal/base/workspaceid"
)

func TestEffectiveCacheContextExplicitWins(t *testing.T) {
	got := (&Config{CacheContext: "my-project"}).EffectiveCacheContext("/whatever")
	if got != "my-project" {
		t.Fatalf("explicit cachecontext = %q, want my-project", got)
	}
}

// TestEffectiveCacheContextUnsetSendsNothing guards the default: with no
// explicit id and no opt-in, nothing identifying reaches a provider.
func TestEffectiveCacheContextUnsetSendsNothing(t *testing.T) {
	if got := (&Config{}).EffectiveCacheContext("/tmp/proj"); got != "" {
		t.Fatalf("unset cachecontext = %q, want empty", got)
	}
	if got := (&Config{}).EffectiveSessionContext("/tmp/proj"); got != "" {
		t.Fatalf("unset session id = %q, want empty", got)
	}
}

func TestEffectiveCacheContextSanitizesExplicit(t *testing.T) {
	got := (&Config{CacheContext: "my project!"}).EffectiveCacheContext("")
	if got != "my-project-" {
		t.Fatalf("sanitized explicit = %q, want my-project-", got)
	}
}

// TestEffectiveCacheContextAutoUsesWorkspaceKey pins the "auto" sentinel: the id
// is the workspace key, sanitized, stable, and free of any username.
func TestEffectiveCacheContextAutoUsesWorkspaceKey(t *testing.T) {
	root := t.TempDir()
	c := &Config{CacheContext: "auto"}
	got := c.EffectiveCacheContext(root)
	want := sanitizeCacheContext(workspaceid.Key(root))
	if got != want {
		t.Fatalf("auto = %q, want workspace key %q", got, want)
	}
	if cacheContextIDRegexp.MatchString(got) {
		t.Fatalf("auto id %q not sanitized", got)
	}
	if again := c.EffectiveCacheContext(root); again != got {
		t.Fatalf("auto id not stable: %q != %q", again, got)
	}
}

// TestEffectiveCacheContextAutoIsExact guards the sentinel against case folding:
// only the literal "auto" derives, so a user can still name a project "Auto".
func TestEffectiveCacheContextAutoIsExact(t *testing.T) {
	got := (&Config{CacheContext: "Auto"}).EffectiveCacheContext("/whatever")
	if got != "Auto" {
		t.Fatalf(`cachecontext "Auto" = %q, want the literal id`, got)
	}
}

func TestEffectiveCacheContextAutoNeedsRoot(t *testing.T) {
	if got := (&Config{CacheContext: "auto"}).EffectiveCacheContext(""); got != "" {
		t.Fatalf("auto without root = %q, want empty", got)
	}
}

// TestEffectiveSessionContextBoundedTo256 guards OpenRouter's session_id
// ceiling: an over-long id is shortened and stays sanitized.
func TestEffectiveSessionContextBoundedTo256(t *testing.T) {
	got := (&Config{CacheContext: strings.Repeat("a", 600)}).EffectiveSessionContext("/x")
	if len(got) != maxSessionContextLen {
		t.Fatalf("session id len = %d, want %d", len(got), maxSessionContextLen)
	}
	if cacheContextIDRegexp.MatchString(got) {
		t.Fatalf("session id %q not sanitized", got)
	}
}

func TestEffectiveSessionContextMatchesShortCacheContext(t *testing.T) {
	root := t.TempDir()
	c := &Config{CacheContext: "auto"}
	if got, want := c.EffectiveSessionContext(root), c.EffectiveCacheContext(root); got != want {
		t.Fatalf("session id %q != cachecontext %q", got, want)
	}
}

func TestBoundContextIDOverLongHashes(t *testing.T) {
	long := strings.Repeat("abc-def-", 200) // 1600 chars
	got := boundContextID(long, maxCacheContextLen)
	if len(got) != maxCacheContextLen {
		t.Fatalf("len = %d, want %d", len(got), maxCacheContextLen)
	}
	if cacheContextIDRegexp.MatchString(got) {
		t.Fatalf("output %q contains rejected characters", got)
	}
	if boundContextID(long, maxCacheContextLen) != got {
		t.Fatal("bounding is not deterministic")
	}
	if boundContextID(long, maxCacheContextLen) == boundContextID(long+"x", maxCacheContextLen) {
		t.Fatal("distinct over-long ids collapsed to one")
	}
}
