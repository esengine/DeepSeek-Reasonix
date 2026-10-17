package persona

import (
	"strings"
	"testing"

	"reasonix/internal/contract/config"
)

func boolPtr(v bool) *bool { return &v }

func TestPresetsAreStableAndHitOnlyOnePlacement(t *testing.T) {
	got := Presets()
	want := []string{"engineer", "concise", "mentor", "critic"}
	if len(got) != len(want) {
		t.Fatalf("Presets() = %d entries, want %d", len(got), len(want))
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("preset %d = %q, want %q", i, got[i].ID, id)
		}
		if got[i].Prefix == "" && got[i].Suffix == "" {
			t.Fatalf("preset %q contributes nothing", id)
		}
		if got[i].Complete {
			t.Fatalf("preset %q is complete; a preset must not replace the prompt", id)
		}
		if !got[i].IncludeRuntimeContext {
			t.Fatalf("preset %q suppresses runtime context; that is an explicit user choice", id)
		}
	}
	// A returned slice is a copy: a caller reordering it must not reorder the
	// shipped order on the next call.
	got[0], got[1] = got[1], got[0]
	if Presets()[0].ID != "engineer" {
		t.Fatal("Presets() exposed the kernel's slice")
	}
}

func TestFromConfigNormalizesAndResolvesRuntimeContext(t *testing.T) {
	if s := FromConfig(config.PersonaConfig{}); (s != Settings{IncludeRuntimeContext: true}) {
		t.Fatalf("zero config = %+v, want runtime context enabled", s)
	}
	s := FromConfig(config.PersonaConfig{Prefix: "  lead  ", Suffix: " trail ", IncludeRuntimeContext: boolPtr(false)})
	if s.Prefix != "lead" || s.Suffix != "trail" || s.IncludeRuntimeContext {
		t.Fatalf("FromConfig = %+v, want trimmed text and runtime context off", s)
	}
}

func TestApplyPrefixAndSuffixRespectCompleteMode(t *testing.T) {
	s := Settings{Prefix: "IDENTITY", Suffix: "TAIL"}
	if got := ApplyPrefix("BASE", s); got != "IDENTITY\n\nBASE" {
		t.Fatalf("ApplyPrefix = %q", got)
	}
	if got := ApplySuffix("BASE", s); got != "BASE\n\nTAIL" {
		t.Fatalf("ApplySuffix = %q", got)
	}
	complete := Settings{Prefix: "ONLY", Suffix: "TAIL", Complete: true}
	if got := ApplyPrefix("BASE", complete); got != "BASE" {
		t.Fatalf("complete ApplyPrefix = %q, want base untouched", got)
	}
	if got := ApplySuffix("BASE", complete); got != "BASE" {
		t.Fatalf("complete ApplySuffix = %q, want base untouched", got)
	}
}

func TestApplyPrefixAndSuffixHandleAnEmptyBase(t *testing.T) {
	s := Settings{Prefix: "IDENTITY", Suffix: "TAIL"}
	if got := strings.TrimSpace(ApplyPrefix("", s)); got != "IDENTITY" {
		t.Fatalf("ApplyPrefix on empty base = %q", got)
	}
	if got := strings.TrimSpace(ApplySuffix("", s)); got != "TAIL" {
		t.Fatalf("ApplySuffix on empty base = %q", got)
	}
}

func TestFinalReplacesEverythingInCompleteMode(t *testing.T) {
	assembled := "BASE\n\npolicy\n\n## Environment"
	if got := Final(Settings{}, assembled); got != assembled {
		t.Fatalf("ordinary Final = %q, want assembled untouched", got)
	}
	if got := Final(Settings{Prefix: "ONLY", Complete: true}, assembled); got != "ONLY" {
		t.Fatalf("complete Final = %q, want exactly the prefix", got)
	}
}
