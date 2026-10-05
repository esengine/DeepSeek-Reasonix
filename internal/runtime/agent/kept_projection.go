package agent

import (
	"reasonix/internal/state/sessionstore"
	"slices"
	"strings"

	"reasonix/internal/contract/provider"
)

// keptForProjection is the form a verbatim-retained message takes in the
// projection. Retention promises the message survives the fold, not that every
// byte it arrived with does: a failure keeps its diagnosis without its passing
// noise.
func (a *contextWindow) keptForProjection(m provider.Message) provider.Message {
	if !failedExecution(m.ToolExecution) {
		return m
	}
	// A recorded selection is a model's reading of this exact output, so it
	// wins over the word list and the position floor, which only approximate it.
	if sel := m.ToolExecution.DiagnosticLines; len(sel) > 0 {
		m.Content = keepFromSelection(m.Content, sel)
		return m
	}
	m.Content = snipFailureTail(m.Content, recordedTailLines(m.ToolExecution))
	return m
}

// supersedeStandingState drops each standing-state block a later retained turn
// repeats. A block is superseded only by a newer one carrying the same tag AND
// the same attributes: a short reminder must never displace the fuller
// statement it points at, or the fold leaves the model following a dangling
// reference. Newest-first, so the survivor is the newest of each variant.
func supersedeStandingState(kept []provider.Message) []provider.Message {
	seen := make(map[string]bool)
	drop := func(tag, opening string) bool {
		if !sessionstore.SupersededUserBlock[tag] || seen[opening] {
			return sessionstore.SupersededUserBlock[tag]
		}
		seen[opening] = true
		return false
	}
	for i, m := range slices.Backward(kept) {
		if !carriesStandingState(m) {
			continue
		}
		kept[i].Content = filterLeadingBlocks(kept[i].Content, drop)
	}
	return kept
}

// StripSupersededUserBlocks is the floor a retained turn can shrink to, with
// every standing block gone. keepUserTurns costs turns against this floor: at
// most one copy of each variant survives the whole fold, so the error is those
// few blocks rather than one per turn.
func StripSupersededUserBlocks(content string) string {
	return filterLeadingBlocks(content, func(tag, _ string) bool {
		return sessionstore.SupersededUserBlock[tag]
	})
}

func carriesStandingState(m provider.Message) bool {
	return m.Role == provider.RoleUser && !m.LocalOnly && !isCompactionSummary(m)
}

// projectedUserTurnFloor is what keepUserTurns measures a retained turn as.
func projectedUserTurnFloor(m provider.Message) provider.Message {
	if !carriesStandingState(m) {
		return m
	}
	m.Content = StripSupersededUserBlocks(m.Content)
	return m
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
	if a == nil || opening == "" {
		return false
	}
	for _, m := range a.window().modelVisibleMessages() {
		if carriesStandingState(m) && strings.Contains(m.Content, opening) {
			return true
		}
	}
	return false
}
