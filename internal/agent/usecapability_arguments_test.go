package agent

import (
	"context"
	"encoding/json"
	"testing"
)

func TestNormalizeMCPToolArguments(t *testing.T) {
	tests := []struct {
		name    string
		raw     json.RawMessage
		want    string
		wantErr bool
	}{
		{name: "object", raw: json.RawMessage(`{"filesToRebuild":["a.go"]}`), want: `{"filesToRebuild":["a.go"]}`},
		{name: "string object", raw: json.RawMessage(`"{\"filesToRebuild\":[\"a.go\"]}"`), wantErr: true},
		{name: "missing", want: `{}`},
		{name: "null", raw: json.RawMessage(`null`), want: `{}`},
		{name: "array", raw: json.RawMessage(`[]`), wantErr: true},
		{name: "number", raw: json.RawMessage(`1`), wantErr: true},
		{name: "boolean", raw: json.RawMessage(`true`), wantErr: true},
		{name: "bad string", raw: json.RawMessage(`"{"`), wantErr: true},
		{name: "nested string", raw: json.RawMessage(`"\"{}\""`), wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeMCPToolArguments(test.raw)
			if test.wantErr {
				if err == nil {
					t.Fatalf("normalize(%s) succeeded with %s", test.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalize(%s): %v", test.raw, err)
			}
			if string(got) != test.want {
				t.Fatalf("normalize(%s) = %s, want %s", test.raw, got, test.want)
			}
		})
	}
}

func TestUseCapabilityRejectsStringWrappedMCPArgumentsBeforeResolution(t *testing.T) {
	proxy := NewUseCapabilityTool(context.Background(), nil, nil, nil, nil, nil, nil)
	_, err := proxy.ResolveCall(t.Context(), json.RawMessage(`{
			"action":"call",
			"capability_id":"mcp-tool:missing/tool",
			"arguments":"{\"value\":1}"
		}`))
	if err == nil {
		t.Fatal("ResolveCall accepted a JSON-string-wrapped object")
	}
}
