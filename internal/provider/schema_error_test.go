package provider

import (
	"errors"
	"strings"
	"testing"
)

func TestAnnotateToolSchemaErrorNamesMCPSource(t *testing.T) {
	err := &APIError{
		Provider: "mimo",
		Status:   400,
		Body:     `{"error":{"message":"Tool 1 function has invalid 'parameters' schema"}}`,
	}
	tools := []ToolSchema{
		{Name: "read_file"},
		{Name: "mcp__filesystem__search"},
	}

	got := AnnotateToolSchemaError(err, tools)
	var apiErr *APIError
	if !errors.As(got, &apiErr) {
		t.Fatalf("AnnotateToolSchemaError() = %T, want *APIError", got)
	}
	for _, want := range []string{`Reasonix tool "mcp__filesystem__search"`, `MCP server "filesystem"`, `tool "search"`} {
		if !strings.Contains(apiErr.ToolContext, want) {
			t.Errorf("ToolContext = %q, want %q", apiErr.ToolContext, want)
		}
	}
}

func TestAnnotateToolSchemaErrorLeavesUnrelatedErrorsUnchanged(t *testing.T) {
	tests := []error{
		errors.New("network down"),
		&APIError{Provider: "mimo", Status: 429, Body: "Tool 0 function failed"},
		&APIError{Provider: "mimo", Status: 400, Body: "other bad request"},
		&APIError{Provider: "mimo", Status: 400, Body: "Tool 9 function has invalid schema"},
	}
	tools := []ToolSchema{{Name: "read_file"}}
	for _, err := range tests {
		if got := AnnotateToolSchemaError(err, tools); got != err {
			t.Errorf("AnnotateToolSchemaError(%v) changed unrelated error to %v", err, got)
		}
	}
}
