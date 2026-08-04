package agent

import (
	"strings"
	"testing"
)

// Every tag the host prepends must strip cleanly, whatever else precedes it.
// A tag missing from TransientUserBlockTags used to reach the UI verbatim —
// <autoresearch-runtime> showed up in session titles and the rewind picker.
func TestStripTransientUserBlocksCoversEveryDeclaredTag(t *testing.T) {
	const prompt = "refactor the parser"
	for _, tag := range TransientUserBlockTags {
		t.Run(tag, func(t *testing.T) {
			block := "<" + tag + ">\nruntime detail\n</" + tag + ">\n\n"
			if got := StripTransientUserBlocks(block + prompt); got != prompt {
				t.Fatalf("StripTransientUserBlocks(%q) = %q, want %q", block+prompt, got, prompt)
			}
			if got := UserPreviewText(block + prompt); !strings.HasPrefix(got, prompt) {
				t.Fatalf("UserPreviewText leaked markup: %q", got)
			}
		})
	}
}

// The blocks arrive stacked (active-goal then autoresearch-runtime then the
// language blocks), so stripping has to consume the whole run, not just the
// first one.
func TestStripTransientUserBlocksConsumesStackedBlocks(t *testing.T) {
	const prompt = "继续执行计划"
	stacked := "<active-goal>\ngoal: ship it\n</active-goal>\n\n" +
		"<autoresearch-runtime>\nstatus: running\n</autoresearch-runtime>\n\n" +
		"<response-language>\nprefer zh\n</response-language>\n\n" +
		prompt
	if got := StripTransientUserBlocks(stacked); got != prompt {
		t.Fatalf("StripTransientUserBlocks = %q, want %q", got, prompt)
	}
}

// Attribute-carrying open tags (hook-context, capability-route) must strip too.
func TestStripTransientUserBlocksHandlesAttributedTags(t *testing.T) {
	const prompt = "run the tests"
	in := `<capability-route version="1">` + "\nroute: test\n</capability-route>\n\n" + prompt
	if got := StripTransientUserBlocks(in); got != prompt {
		t.Fatalf("StripTransientUserBlocks = %q, want %q", got, prompt)
	}
}

// hasLeadingInjectedBlock walks the same list, so a block already present
// behind other injected blocks is detected instead of being added twice.
func TestHasLeadingInjectedBlockSkipsEveryDeclaredTag(t *testing.T) {
	const target = "reasoning-language"
	for _, tag := range TransientUserBlockTags {
		if tag == target {
			continue
		}
		t.Run(tag, func(t *testing.T) {
			content := "<" + tag + ">\nx\n</" + tag + ">\n\n" +
				"<" + target + ">\nprefer zh\n</" + target + ">\n\nhello"
			if !hasLeadingInjectedBlock(content, target) {
				t.Fatalf("hasLeadingInjectedBlock(%q) = false, want the existing %s block detected", content, target)
			}
		})
	}
}

func TestHasLeadingInjectedBlockIgnoresUserProse(t *testing.T) {
	if hasLeadingInjectedBlock("what does <response-language> mean?", "response-language") {
		t.Fatal("prose mentioning a tag must not count as an injected block")
	}
	if hasLeadingInjectedBlock("<active-goal>\ng\n</active-goal>\n\nplain text", "reasoning-language") {
		t.Fatal("walking past other blocks must not invent a target block")
	}
}
