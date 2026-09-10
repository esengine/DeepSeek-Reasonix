package agent

import (
	"encoding/json"
	"fmt"
)

type incompleteReadGateInput struct {
	result    toolResultReadParams
	read      readFileArgs
	grep      incompleteReadGrepArgs
	receipt   readStrategyReceiptArgs
	resultOK  bool
	readOK    bool
	grepOK    bool
	receiptOK bool
}

func parseIncompleteReadGateInput(plan *toolCallPlan) incompleteReadGateInput {
	var input incompleteReadGateInput
	input.resultOK = plan.evidenceName == "session_tool_result" && json.Unmarshal(plan.evidenceArgs, &input.result) == nil
	input.read, input.readOK = parseReadFileArgs(plan.evidenceArgs)
	input.grep, input.grepOK = parseIncompleteReadGrepArgs(plan.evidenceArgs)
	input.receipt, input.receiptOK = parseReadStrategyReceiptArgs(plan.evidenceArgs)
	return input
}

func matchIncompleteReadGateEntry(plan *toolCallPlan, input incompleteReadGateInput, key string, entry *incompleteRead) (string, bool, bool) {
	switch entry.phase {
	case incompleteReadAutoResultPage:
		return matchIncompleteReadResultPage(plan, input, key, entry, incompleteReadActionAutoResult, "incomplete")
	case incompleteReadAutoSourcePage:
		return matchIncompleteReadSourcePage(plan, input, key, entry, incompleteReadActionAutoSource, "incomplete")
	case incompleteReadStrategyResultPage:
		message, matched, _ := matchIncompleteReadResultPage(plan, input, key, entry, incompleteReadActionStrategyResult, "targeted")
		return message, matched, true
	case incompleteReadStrategySourcePage:
		message, matched, _ := matchIncompleteReadSourcePage(plan, input, key, entry, incompleteReadActionStrategySource, "targeted")
		return message, matched, true
	case incompleteReadStrategy:
		return "", matchReadStrategyAction(plan, input, key, entry), true
	default:
		return "", false, false
	}
}

func matchIncompleteReadResultPage(plan *toolCallPlan, input incompleteReadGateInput, key string, entry *incompleteRead, action incompleteReadAction, label string) (string, bool, bool) {
	if !input.resultOK || input.result.ToolCallID != entry.toolCallID {
		return "", false, false
	}
	if input.result.ResultRef != entry.resultRef || input.result.Offset != entry.nextByteOffset {
		return fmt.Sprintf("blocked: %s read continuation mismatch for tool_call_id %q; require result_ref=%q offset=%d", label, entry.toolCallID, entry.resultRef, entry.nextByteOffset), true, false
	}
	plan.incompleteReadRoot = key
	plan.incompleteReadAction = action
	return "", true, false
}

func matchIncompleteReadSourcePage(plan *toolCallPlan, input incompleteReadGateInput, key string, entry *incompleteRead, action incompleteReadAction, label string) (string, bool, bool) {
	if plan.evidenceName != "read_file" || !input.readOK || !readPathMatches(input.read.Path, entry) {
		return "", false, false
	}
	if input.read.Offset != entry.nextSourceOffset || !input.read.LimitExplicit || input.read.Limit != entry.nextSourceLimit {
		return fmt.Sprintf("blocked: %s source continuation mismatch for path %q; require offset=%d limit=%d", label, entry.path, entry.nextSourceOffset, entry.nextSourceLimit), true, false
	}
	plan.incompleteReadRoot = key
	plan.incompleteReadAction = action
	return "", true, false
}

func matchReadStrategyAction(plan *toolCallPlan, input incompleteReadGateInput, key string, entry *incompleteRead) bool {
	var action incompleteReadAction
	switch {
	case plan.evidenceName == "grep" && input.grepOK && readPathMatches(input.grep.Path, entry):
		action = incompleteReadActionStrategySearch
	case plan.evidenceName == "read_file" && input.readOK && input.read.OffsetExplicit && input.read.LimitExplicit && readPathMatches(input.read.Path, entry):
		action = incompleteReadActionStrategyRead
	case plan.evidenceName == "session_read_strategy_receipt" && input.receiptOK && input.receipt.ReadID == entry.readID:
		action = incompleteReadActionStrategyReceipt
	default:
		return false
	}
	plan.incompleteReadRoot = key
	plan.incompleteReadAction = action
	return true
}

func (s *incompleteReadState) nextInstruction() string {
	return s.instructionFor("")
}

func (s *incompleteReadState) instructionFor(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.firstLocked()
	if key != "" {
		entry = s.entries[key]
	}
	if entry == nil {
		return ""
	}
	if key == "" {
		hint := fmt.Sprintf("%s/%d/%d/%d/%d/%d", entry.key, entry.phase, entry.nextByteOffset, entry.nextSourceOffset, len(entry.searches), len(entry.reads))
		if hint == s.lastHint {
			return ""
		}
		s.lastHint = hint
	}
	switch entry.phase {
	case incompleteReadAutoResultPage, incompleteReadStrategyResultPage:
		args, _ := json.Marshal(map[string]any{"tool_call_id": entry.toolCallID, "result_ref": entry.resultRef, "offset": entry.nextByteOffset, "limit": toolResultPageMaxBytes})
		return fmt.Sprintf("The host retained a PARTIAL read_file result. If more content is needed, call use_capability with action=\"call\", capability_id=\"session:tool_result\", arguments=%s. Independent work may continue; an explicit full-file request still requires complete coverage.", args)
	case incompleteReadAutoSourcePage, incompleteReadStrategySourcePage:
		args, _ := json.Marshal(map[string]any{"path": entry.path, "offset": entry.nextSourceOffset, "limit": entry.nextSourceLimit})
		return fmt.Sprintf("The read_file window has more source lines. Continue with read_file %s if needed. A partial window may be sufficient for local work; an explicit full-file request still requires complete coverage.", args)
	case incompleteReadStrategy:
		readExample, _ := json.Marshal(map[string]any{"path": entry.path, "offset": 0, "limit": 200})
		receiptExample, _ := json.Marshal(map[string]any{"read_id": entry.readID, "search_tool_call_ids": []string{"<grep tool call id>"}, "read_tool_call_ids": []string{"<read_file tool call id>"}, "conclusion": "<why these searches and exact windows are sufficient for the task>"})
		return fmt.Sprintf("The complete file cannot fit the dynamic context budget. READ STRATEGY read_id=%q path=%q. Independent work may continue. Search this file with grep, then read relevant windows (example %s). A targeted receipt never proves whole-file coverage. Record the targeted scope using use_capability with action=\"call\", capability_id=\"session:read_strategy_receipt\", arguments=%s.", entry.readID, entry.path, readExample, receiptExample)
	default:
		return ""
	}
}
