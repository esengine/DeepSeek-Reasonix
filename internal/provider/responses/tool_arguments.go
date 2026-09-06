package responses

import "bytes"

type streamedCall struct {
	id, name, arguments string
	argumentBuffer      bytes.Buffer
	buffered            bool
	argChars            int
	completed           bool
}

func (c *streamedCall) appendArguments(fragment string) {
	if fragment == "" {
		return
	}
	if c.buffered {
		c.argumentBuffer.WriteString(fragment)
		return
	}
	if c.arguments == "" {
		c.arguments = fragment
		return
	}
	c.buffered = true
	c.argumentBuffer.Grow(len(c.arguments) + len(fragment))
	c.argumentBuffer.WriteString(c.arguments)
	c.argumentBuffer.WriteString(fragment)
	c.arguments = ""
}

func (c *streamedCall) setArguments(arguments string) {
	c.arguments = arguments
	c.argumentBuffer = bytes.Buffer{}
	c.buffered = false
}

func (c *streamedCall) completeArguments() string {
	if c.buffered {
		c.arguments = c.argumentBuffer.String()
		c.argumentBuffer = bytes.Buffer{}
		c.buffered = false
	}
	return c.arguments
}
