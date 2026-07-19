package builtin

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestJsonFormatToolName(t *testing.T) {
	tool := jsonFormatTool{}
	if got, want := tool.Name(), "json_format"; got != want {
		t.Fatalf("Name() = %q, want %q", got, want)
	}
}

func TestJsonFormatToolReadOnly(t *testing.T) {
	tool := jsonFormatTool{}
	if !tool.ReadOnly() {
		t.Fatal("ReadOnly() = false, want true")
	}
}

func TestJsonFormatToolExecute(t *testing.T) {
	tool := jsonFormatTool{}

	cases := []struct {
		name         string
		input        string
		indent       int
		minify       bool
		wantContains string
		wantErr      bool
	}{
		{
			name:         "simple",
			input:        `{"b":2,"a":1}`,
			indent:       2,
			wantContains: "\n  \"a\": 1",
		},
		{
			name:         "nested",
			input:        `{"x":{"y":{"z":true}}}`,
			indent:       2,
			wantContains: "    \"z\": true",
		},
		{
			name:         "minify",
			input:        `{"a":1,"b":2}`,
			minify:       true,
			wantContains: `{"a":1,"b":2}`,
		},
		{
			name:         "indent_zero",
			input:        `{"a":1}`,
			indent:       0,
			wantContains: "\"a\": 1",
		},
		{
			name:    "invalid_json",
			input:   `not json`,
			indent:  2,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := t.TempDir() + "/input.json"
			if err := os.WriteFile(path, []byte(tc.input), 0644); err != nil {
				t.Fatal(err)
			}

			args, err := json.Marshal(map[string]any{
				"input_file": path,
				"indent":     tc.indent,
				"minify":     tc.minify,
			})
			if err != nil {
				t.Fatal(err)
			}

			out, err := tool.Execute(context.Background(), args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got output: %s", out)
				}
				if !strings.Contains(err.Error(), "invalid JSON") {
					t.Fatalf("expected invalid JSON error, got: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(out, tc.wantContains) {
				t.Fatalf("output %q does not contain %q", out, tc.wantContains)
			}
		})
	}
}
