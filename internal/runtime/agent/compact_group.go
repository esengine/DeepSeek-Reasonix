package agent

import "reasonix/internal/contract/provider"

// splittable reports whether a turn's calls can be told apart by id. Without
// distinct ids a result cannot be matched to its call, so the turn stays whole.
func splittable(m provider.Message) bool {
	seen := make(map[string]bool, len(m.ToolCalls))
	for _, tc := range m.ToolCalls {
		if tc.ID == "" || seen[tc.ID] {
			return false
		}
		seen[tc.ID] = true
	}
	return true
}

// keepToolCallGroup retains a calling turn with every result it received.
func keepToolCallGroup(region []provider.Message, keep []bool, assistantIndex int) {
	if assistantIndex < 0 || assistantIndex >= len(region) {
		return
	}
	m := region[assistantIndex]
	if m.Role != provider.RoleAssistant || len(m.ToolCalls) == 0 {
		return
	}
	keep[assistantIndex] = true
	ids := toolCallIDs(m)
	for j := assistantIndex + 1; j < len(region) && region[j].Role == provider.RoleTool; j++ {
		if ids[region[j].ToolCallID] {
			keep[j] = true
		}
	}
}

// keepAnsweredCall retains the turn that issued the call a kept result
// answers. Its other results stay free to fold when the calls can be told
// apart; splitKeptTurn then hands each side only its own calls.
func keepAnsweredCall(region []provider.Message, keep []bool, assistantIndex int) {
	if assistantIndex >= 0 && assistantIndex < len(region) && splittable(region[assistantIndex]) {
		keep[assistantIndex] = true
		return
	}
	keepToolCallGroup(region, keep, assistantIndex)
}

// splitKeptTurn splits a kept assistant turn whose results are not all
// kept: the retained copy issues only the calls whose results stay, and the
// folded copy carries the rest so the summarizer and the coverage guard still
// see them. Neither side issues a call the other side answers.
func splitKeptTurn(region []provider.Message, keep []bool, i int) (kept provider.Message, folded *provider.Message) {
	m := region[i]
	if len(m.ToolCalls) == 0 || !splittable(m) {
		return m, nil
	}
	answered := map[string]bool{}
	for j := i + 1; j < len(region) && region[j].Role == provider.RoleTool; j++ {
		if keep[j] {
			answered[region[j].ToolCallID] = true
		}
	}
	var stay, fold []provider.ToolCall
	for _, tc := range m.ToolCalls {
		if answered[tc.ID] {
			stay = append(stay, tc)
		} else {
			fold = append(fold, tc)
		}
	}
	if len(fold) == 0 {
		return m, nil
	}
	m.ToolCalls = stay
	return m, &provider.Message{Role: provider.RoleAssistant, CreatedAt: m.CreatedAt, ToolCalls: fold}
}
