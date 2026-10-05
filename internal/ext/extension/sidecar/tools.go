package sidecar

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"reasonix/internal/ext/extension/protocol"
	"reasonix/internal/ext/pluginpkg"
)

// validateTools holds the tools a runtime activated at initialize to the ones
// its manifest declares: the model is shown the manifest's schemas, so a tool
// the manifest does not describe has nothing the model could have called.
func (c *Client) validateTools(served []string) error {
	if len(served) == 0 {
		return nil
	}
	if !containsString(c.rt.Capabilities, "tools") {
		return &protocol.ProtocolError{Reason: protocol.ErrCapabilityNotDeclared, Message: fmt.Sprintf("extension %s served tools without the tools capability", c.pluginID)}
	}
	for _, name := range served {
		if !c.declaresTool(name) {
			return &protocol.ProtocolError{Reason: protocol.ErrCapabilityNotDeclared, Message: fmt.Sprintf("extension %s served tool %q which its manifest does not declare", c.pluginID, name)}
		}
	}
	return nil
}

func (c *Client) declaresTool(name string) bool {
	for _, t := range c.rt.Tools {
		if t.Name == name {
			return true
		}
	}
	return false
}

// ServesTool reports whether the runtime activated a declared tool.
func (c *Client) ServesTool(name string) bool {
	return containsString(c.Handshake().Tools, name)
}

// CallTool runs one call of a declared tool and bounds it by timeout.
func (c *Client) CallTool(ctx context.Context, name string, args json.RawMessage, timeout time.Duration) (protocol.ToolCallResult, error) {
	if err := c.readyErr(); err != nil {
		return protocol.ToolCallResult{}, err
	}
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	raw, err := c.conn.Request(ctx, string(protocol.MethodExtensionToolCall), protocol.ToolCallParams{
		Name: name, Arguments: args, TimeoutMillis: int(timeout / time.Millisecond),
	})
	if err != nil {
		return protocol.ToolCallResult{}, mapRequestError(err)
	}
	decoded, err := protocol.DecodeHostRequestResult(protocol.MethodExtensionToolCall, raw)
	if err != nil {
		return protocol.ToolCallResult{}, &protocol.ProtocolError{Reason: protocol.ErrProtocolError, Message: "invalid tool result: " + err.Error()}
	}
	return decoded.(protocol.ToolCallResult), nil
}

// DeclaredTools returns the tools this runtime's manifest declares.
func (c *Client) DeclaredTools() []pluginpkg.RuntimeTool {
	return append([]pluginpkg.RuntimeTool(nil), c.rt.Tools...)
}
