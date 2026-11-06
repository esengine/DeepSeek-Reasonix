package agent

import (
	"reasonix/internal/state/sessionstore"
	"slices"
	"strings"

	"reasonix/internal/contract/provider"
)

// supersedeStandingState drops each standing-state block a later turn — kept or
// in the verbatim tail — repeats. Only a newer block with the same tag AND the
// same attributes supersedes: a short reminder must never displace the fuller
// statement it points at. Newest-first, so the newest of each variant survives.
func supersedeStandingState(kept, tail []provider.Message) []provider.Message {
	seen := make(map[string]bool)
	drop := func(tag, opening string) bool {
		if !sessionstore.SupersededUserBlock[tag] || seen[opening] {
			return sessionstore.SupersededUserBlock[tag]
		}
		seen[opening] = true
		return false
	}
	for _, m := range tail {
		if carriesStandingState(m) {
			filterLeadingBlocks(m.Content, drop)
		}
	}
	for i, m := range slices.Backward(kept) {
		if !carriesStandingState(m) {
			continue
		}
		kept[i].Content = filterLeadingBlocks(kept[i].Content, drop)
	}
	return kept
}

func carriesStandingState(m provider.Message) bool {
	return m.Role == provider.RoleUser && !m.LocalOnly && !isCompactionSummary(m)
}

// filterLeadingBlocks walks the host-injected block chain that opens content and
// drops the ones drop reports. It walks the chain rather than matching anywhere
// in the text: a block the host did not inject is the user's own prose, and a
// one-time block ahead of a standing one must not hide it.
func filterLeadingBlocks(content string, drop func(tag, opening string) bool) string {
	rest := content
	var kept strings.Builder
	for {
		tag, opening, end, ok := leadingTransientBlock(rest)
		if !ok {
			break
		}
		if !drop(tag, opening) {
			kept.WriteString(rest[:end])
		}
		rest = rest[end:]
	}
	out := kept.String() + rest
	// A turn that was nothing but standing state still has to stay a turn: an
	// empty user message is a worse projection than a redundant one.
	if strings.TrimSpace(out) == "" {
		return content
	}
	return strings.TrimLeft(out, " \t\r\n")
}

// leadingTransientBlock reports which host-injected block opens s, its exact
// opening tag (the variant key, attributes included), and where it ends. The
// tag is read from the opening because RE2 cannot backreference a capture.
func leadingTransientBlock(s string) (tag, opening string, end int, ok bool) {
	loc := sessionstore.ReTransientUserBlock.FindStringIndex(s)
	if loc == nil || loc[0] != 0 {
		return "", "", 0, false
	}
	head := strings.TrimLeft(s[:loc[1]], " \t\r\n")
	close := strings.IndexByte(head, '>')
	if close < 0 {
		return "", "", 0, false
	}
	for _, t := range sessionstore.TransientUserBlockTags {
		if strings.HasPrefix(head, "<"+t+">") || strings.HasPrefix(head, "<"+t+" ") {
			return t, head[:close+1], loc[1], true
		}
	}
	return "", "", 0, false
}

// SeesStandingBlock reports whether the model-visible history still carries a
// block opening with exactly this tag. A host that restates standing state asks
// rather than remembering it once sent: a fold, a rewind, or a resumed session
// all change the answer without telling anyone.
func (a *Agent) SeesStandingBlock(opening string) bool {
	return a.seesVisible(opening)
}

// SeesProjection reports whether the model-visible history still carries text
// verbatim. Delivery of a once-only fact is asked this way rather than
// remembered at the hand-off: a turn refused before the provider, a cancelled
// turn whose prefixes were stripped, a fold, a rewind and a session rotation
// all change the answer by themselves.
func (a *Agent) SeesProjection(text string) bool {
	return a.seesVisible(text)
}

// NewestStandingBlock returns the most recent host-injected block with tag in
// the model-visible history, verbatim, and whether one is there. A host that
// restates a block asks this rather than remembering the last bytes it sent:
// the newest block of a tag is what the model currently holds, so an older copy
// of the same bytes is not delivery when a newer one superseded them.
func (a *Agent) NewestStandingBlock(tag string) (string, bool) {
	if a == nil || tag == "" {
		return "", false
	}
	for _, m := range slices.Backward(a.window().modelVisibleMessages()) {
		if !carriesStandingState(m) {
			continue
		}
		if block, ok := lastLeadingBlock(m.Content, tag); ok {
			return block, true
		}
	}
	return "", false
}

// lastLeadingBlock returns the last block with tag in content's leading chain,
// trimmed to the bytes the host rendered. Walking the chain rather than
// matching anywhere keeps a tag the user typed in their own prose out of it.
func lastLeadingBlock(content, tag string) (string, bool) {
	rest := content
	var block string
	for {
		got, _, end, ok := leadingTransientBlock(rest)
		if !ok {
			break
		}
		if got == tag {
			block = strings.TrimRight(rest[:end], " \t\r\n")
		}
		rest = rest[end:]
	}
	return block, block != ""
}

// seesVisible is the one scan both questions share: only a user record the
// model reads counts, so LocalOnly output and compaction summaries never answer
// for state nobody sent it.
func (a *Agent) seesVisible(text string) bool {
	if a == nil || text == "" {
		return false
	}
	for _, m := range a.window().modelVisibleMessages() {
		if carriesStandingState(m) && strings.Contains(m.Content, text) {
			return true
		}
	}
	return false
}
