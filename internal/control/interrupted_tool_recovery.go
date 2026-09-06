package control

import "reasonix/internal/provider"

func recordInterruptedAssistantRecovery(r *provider.InterruptedTurnRecovery, msgs []provider.Message, i int) {
	results := make(map[string]provider.Message)
	for j := i + 1; j < len(msgs) && msgs[j].Role == provider.RoleTool && !msgs[j].LocalOnly; j++ {
		result := msgs[j]
		results[result.ToolCallID+"\x00"+result.Name] = result
	}
	for _, call := range msgs[i].ToolCalls {
		state := provider.ToolRunUnknown
		if result, ok := results[call.ID+"\x00"+call.Name]; ok {
			state = provider.ToolResultRunState(result)
		}
		provider.RecordToolRecovery(r, interruptedToolSummary(call), state)
		record := provider.ToolCallRecord{
			ID: call.ID, Name: call.Name, Arguments: append([]byte(nil), call.Arguments...), State: state,
		}
		if state == provider.ToolRunUnknown {
			r.RequiresUserDecision = true
		}
		r.ToolCalls = append(r.ToolCalls, record)
	}
}
