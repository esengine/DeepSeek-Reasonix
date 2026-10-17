package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync/atomic"
)

type compositionToolGuard struct {
	fingerprint [sha256.Size]byte
	invalid     atomic.Bool
}

func (c *Client) checkCompositionContract(fingerprint [sha256.Size]byte) error {
	guard := c.compositionGuard.Load()
	if guard == nil {
		return nil
	}
	if fingerprint != guard.fingerprint {
		guard.invalid.Store(true)
		return fmt.Errorf("MCP %s tool contract differs from the session binding", c.name)
	}
	guard.invalid.Store(false)
	return nil
}

func (c *Client) compositionContractError() error {
	if guard := c.compositionGuard.Load(); guard != nil && guard.invalid.Load() {
		return fmt.Errorf("MCP %s tool contract changed; this session cannot continue", c.name)
	}
	return nil
}

// PinCompositionTools locks the first catalog and rejects drift after refresh or reconnect.
func (h *Host) PinCompositionTools(name, expected string) (string, error) {
	c := h.lookupClient(name)
	if c == nil {
		return "", fmt.Errorf("MCP %s is not connected", name)
	}
	c.toolsMu.Lock()
	fingerprint := c.toolsFingerprint
	c.toolsMu.Unlock()
	if expected != "" && hex.EncodeToString(fingerprint[:]) != expected {
		return "", fmt.Errorf("MCP %s tool contract differs from the session binding", name)
	}
	guard := &compositionToolGuard{fingerprint: fingerprint}
	h.mu.Lock()
	if h.compositionGuards == nil {
		h.compositionGuards = map[string]*compositionToolGuard{}
	}
	h.compositionGuards[name] = guard
	h.mu.Unlock()
	c.toolsMu.Lock()
	defer c.toolsMu.Unlock()
	c.compositionGuard.Store(guard)
	if c.toolsFingerprint != fingerprint {
		guard.invalid.Store(true)
		return "", fmt.Errorf("MCP %s tool contract changed while binding", name)
	}
	return hex.EncodeToString(fingerprint[:]), nil
}

func (h *Host) attachCompositionGuard(c *Client) {
	h.mu.RLock()
	guard := h.compositionGuards[c.name]
	h.mu.RUnlock()
	c.compositionGuard.Store(guard)
}

func (h *Host) CompositionContractError() error {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for name, guard := range h.compositionGuards {
		if guard.invalid.Load() {
			return fmt.Errorf("MCP %s tool contract changed", name)
		}
	}
	return nil
}
