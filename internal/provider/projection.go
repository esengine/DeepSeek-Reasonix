package provider

// Two copies of a transcript are derived from the stored one: the bytes a
// provider receives, and the projection compaction writes back. They differ in
// exactly one field, and that difference is load-bearing.

// ModelMessages removes durable display-only records before a request is
// handed to any provider. Healthy sessions without such records keep their
// original backing slice, preserving the allocation and prompt-cache fast path.
func ModelMessages(msgs []Message) []Message { return projectMessages(msgs, false) }

// ProjectionMessages is ModelMessages for a stored projection, except that
// ToolExecution survives: a projection is also the next compaction's input, and
// only that record says a tool call failed. Stripping it here would leave the
// pass after next unable to classify the failure, so the strip belongs at the
// provider boundary, which every request path already crosses.
func ProjectionMessages(msgs []Message) []Message { return projectMessages(msgs, true) }

func projectMessages(msgs []Message, keepExecution bool) []Message {
	needsCopy := false
	for _, m := range msgs {
		if m.LocalOnly || m.RawContent != "" || m.ProviderContent != "" || m.DecisionReceipt != nil || len(m.DecisionReceipts) > 0 || (m.ToolExecution != nil && !keepExecution) {
			needsCopy = true
			break
		}
	}
	if !needsCopy {
		return msgs
	}
	out := make([]Message, 0, len(msgs))
	for _, candidate := range msgs {
		if candidate.LocalOnly {
			continue
		}
		if candidate.ProviderContent != "" {
			candidate.Content = candidate.ProviderContent
			candidate.ProviderContent = ""
		}
		candidate.RawContent = ""
		candidate.DecisionReceipt = nil
		candidate.DecisionReceipts = nil
		if !keepExecution {
			// Local shell metadata must never enter provider request bytes.
			candidate.ToolExecution = nil
		}
		out = append(out, candidate)
	}
	return out
}
