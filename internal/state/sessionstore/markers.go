package sessionstore

import (
	"strings"
)

// HandoffTask returns the original user task embedded in an executor handoff
// message, or s unchanged when it is not one. Session previews and auto-titles
// use it so dual-model sessions surface the user's words, not the handoff
// boilerplate (#3860).
func HandoffTask(s string) string {
	trimmed := strings.TrimSpace(s)
	if !strings.HasPrefix(trimmed, "# "+ExecutorHandoffMarker) {
		return s
	}
	const header = "Original task:\n"
	_, after, ok := strings.Cut(trimmed, header)
	if !ok {
		return s
	}
	rest := after
	if j := strings.Index(rest, "\n\nPlanner output:"); j >= 0 {
		rest = rest[:j]
	}
	if task := strings.TrimSpace(rest); task != "" {
		return task
	}
	return s
}

const ExecutorHandoffMarker = "Reasonix executor handoff"

// DeliveryRuntimeMarker is the retired delivery contract block. Nothing
// appends it any more; it survives byte-exact so a session recorded before
// the retirement still strips it out of previews and steer replay.
const DeliveryRuntimeMarker = `<delivery-runtime>
This session is in delivery-first mode. Before any state-changing tool call,
establish concrete, verifiable acceptance criteria with todo_write. After the
change, inspect the result, run relevant verification, and sign off each step
with complete_step citing the successful verification command. The host enforces
these gates and will reject mutation or finalization when evidence is missing.
</delivery-runtime>`
