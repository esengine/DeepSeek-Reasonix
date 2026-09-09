package provider

import "errors"

// ErrEmptyResponse marks a clean provider completion that carried no text,
// reasoning, tool calls, response items, or server-side activity. The agent
// may safely retry the frozen request because the empty attempt produced no
// assistant message that can be committed to conversation history.
var ErrEmptyResponse = errors.New("empty provider response")
