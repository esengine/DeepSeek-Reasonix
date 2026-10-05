package event

import "reasonix/internal/base/tokencount"

// ContextTokens estimates what this call left in the prompt — arguments plus
// the result the model reads back, which every later turn pays for again. A
// share of the round's usage would read the same on every call in it. A method
// and not a field because ten sites emit ToolResult, and one of them forgetting
// to fill it is a card that reads as free.
func (t Tool) ContextTokens() int {
	// Arguments are still streaming on a partial dispatch, so any figure here
	// would only shrink as the call turns out to be bigger.
	if t.Partial {
		return 0
	}
	return tokencount.Text(t.Args) + tokencount.Text(t.Output) + tokencount.Text(t.Err)
}
