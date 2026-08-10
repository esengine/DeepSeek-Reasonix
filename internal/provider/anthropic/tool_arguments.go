package anthropic

import (
	"bytes"

	"reasonix/internal/provider"
)

type streamedToolCall struct {
	provider.ToolCall
	argumentBuffer bytes.Buffer
	buffered       bool
}

func (c *streamedToolCall) appendArguments(fragment string) {
	if fragment == "" {
		return
	}
	if c.buffered {
		c.argumentBuffer.WriteString(fragment)
		return
	}
	if c.Arguments == "" {
		c.Arguments = fragment
		return
	}
	c.buffered = true
	c.argumentBuffer.Grow(len(c.Arguments) + len(fragment))
	c.argumentBuffer.WriteString(c.Arguments)
	c.argumentBuffer.WriteString(fragment)
	c.Arguments = ""
}

func (c *streamedToolCall) argumentLen() int {
	if c.buffered {
		return c.argumentBuffer.Len()
	}
	return len(c.Arguments)
}

func (c *streamedToolCall) complete() *provider.ToolCall {
	if c.buffered {
		c.Arguments = c.argumentBuffer.String()
		c.argumentBuffer = bytes.Buffer{}
		c.buffered = false
	}
	return &c.ToolCall
}
