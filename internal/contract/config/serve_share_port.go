package config

import (
	"errors"
	"fmt"
	"strings"
)

const (
	MinSharePort = 1024
	MaxSharePort = 65535
)

// ErrSharePortOutOfRange marks a share port outside the unprivileged range.
var ErrSharePortOutOfRange = errors.New("share port out of range")

// ValidateSharePort accepts zero (a free port each time) or an unprivileged port.
func ValidateSharePort(p int) error {
	if p == 0 || (p >= MinSharePort && p <= MaxSharePort) {
		return nil
	}
	return fmt.Errorf("%w: use 0 or a port between %d and %d", ErrSharePortOutOfRange, MinSharePort, MaxSharePort)
}

// SharePort is the fixed port for the phone-access link, or zero when unset or
// hand-written outside the accepted range.
func (c *Config) SharePort() int {
	if c == nil || ValidateSharePort(c.Serve.SharePort) != nil {
		return 0
	}
	return c.Serve.SharePort
}

// SetSharePort validates and stores the port.
func (c *Config) SetSharePort(p int) error {
	if err := ValidateSharePort(p); err != nil {
		return err
	}
	c.Serve.SharePort = p
	return nil
}

// renderServeSection always writes the header: a save that clears share_port
// then removes the key, where an absent table would take a hand-written token
// or hash with it. Those stay in the file and are never re-rendered.
func renderServeSection(b *strings.Builder, c *Config) {
	b.WriteString("[serve]\n")
	if c.Serve.SharePort != 0 {
		fmt.Fprintf(b, "share_port = %d   # fixed port of the phone-access LAN link; unset picks a free one each time\n\n", c.Serve.SharePort)
		return
	}
	b.WriteString("# share_port = 41234   # fixed port of the phone-access LAN link; unset picks a free one each time\n\n")
}
