package provider

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var providerToolIndexPattern = regexp.MustCompile(`(?i)\btool\s+(\d+)\s+function\b`)

// AnnotateToolSchemaError resolves provider messages such as "Tool 197 function
// has invalid 'parameters' schema" back to the stable Reasonix tool identity.
// MCP tool names carry their source server in mcp__<server>__<tool> form, so the
// resulting diagnostic tells users which integration supplied the bad schema.
func AnnotateToolSchemaError(err error, tools []ToolSchema) error {
	var apiErr *APIError
	if !errors.As(err, &apiErr) || (apiErr.Status != 400 && apiErr.Status != 422) {
		return err
	}
	match := providerToolIndexPattern.FindStringSubmatch(apiErr.Body)
	if len(match) != 2 {
		return err
	}
	index, parseErr := strconv.Atoi(match[1])
	if parseErr != nil || index < 0 || index >= len(tools) {
		return err
	}

	tool := tools[index]
	context := fmt.Sprintf("Provider tool %d maps to Reasonix tool %q.", index, tool.Name)
	if server, rawName, ok := splitMCPToolName(tool.Name); ok {
		context = fmt.Sprintf("Provider tool %d maps to Reasonix tool %q (MCP server %q, tool %q).", index, tool.Name, server, rawName)
	}
	annotated := *apiErr
	annotated.ToolContext = context
	return &annotated
}

func splitMCPToolName(name string) (server, tool string, ok bool) {
	const prefix = "mcp__"
	if !strings.HasPrefix(name, prefix) {
		return "", "", false
	}
	parts := strings.SplitN(strings.TrimPrefix(name, prefix), "__", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}
