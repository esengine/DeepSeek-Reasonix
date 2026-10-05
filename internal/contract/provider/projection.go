package provider

import "fmt"

// Two copies of a transcript are derived from the stored one: the bytes a
// provider receives, and the projection compaction writes back. They differ in
// exactly one field, and that difference is load-bearing.

// ModelMessages removes durable display-only records before a request is
// handed to any provider. Healthy sessions without such records keep their
// original backing slice, preserving the allocation and prompt-cache fast path.
func ModelMessages(msgs []Message) []Message { return projectMessages(msgs, false) }

// ProjectionMessages is ModelMessages for a stored projection, except that
// host records survive: ToolExecution because only it says a tool call failed,
// and UnavailableImages because a stored copy may yet come back. A projection
// is the next compaction's input, so both are stripped, or rendered, at the
// provider boundary, which every request path already crosses.
func ProjectionMessages(msgs []Message) []Message { return projectMessages(msgs, true) }

func projectMessages(msgs []Message, keepExecution bool) []Message {
	needsCopy := false
	for _, m := range msgs {
		if (len(m.UnavailableImages) > 0 && !keepExecution) || m.LocalOnly || m.RawContent != "" || m.ProviderContent != "" || m.HostAuthored || m.ModelRef != "" || m.Via != nil || m.DecisionReceipt != nil || len(m.DecisionReceipts) > 0 || (m.ToolExecution != nil && !keepExecution) || (m.ToolFailure != nil && !keepExecution) {
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
		candidate.HostAuthored = false
		candidate.ModelRef = ""
		candidate.Via = nil
		candidate.DecisionReceipt = nil
		candidate.DecisionReceipts = nil
		if !keepExecution {
			// Local shell metadata must never enter provider request bytes.
			candidate.ToolExecution = nil
			candidate.ToolFailure = nil
			if n := len(candidate.UnavailableImages); n > 0 {
				candidate.Content += fmt.Sprintf(UnavailableImagesNote, n)
				candidate.UnavailableImages = nil
			}
		}
		out = append(out, candidate)
	}
	return out
}

// UnavailableImagesNote is what a request says in place of images the host
// could not read back, so the model is not left to guess why they are gone.
const UnavailableImagesNote = "\n[%d image(s) unavailable: the stored copy is missing]"
