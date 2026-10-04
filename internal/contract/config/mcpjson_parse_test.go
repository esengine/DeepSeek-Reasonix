package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestParseMCPServersJSONOmitsEmptyDefinitions(t *testing.T) {
	for _, wrapped := range []bool{false, true} {
		for _, spec := range []string{`{}`, `null`, `{"type":"http"}`, `{"env":{"LABEL":"example"},"auto_start":false}`} {
			body := `{"empty":` + spec + `}`
			if wrapped {
				body = `{"mcpServers":` + body + `}`
			}
			if got, err := ParseMCPServersJSON([]byte(body)); err == nil || len(got) != 0 {
				t.Errorf("ParseMCPServersJSON(%s) = %+v, %v; want no entries and an error", body, got, err)
			}
		}
		body := `{"empty":{},"remote":{"type":"sse","url":"https://example.com/mcp","headers":{"X-Example":"value"}},"local":{"command":"example-mcp","args":["--stdio"],"env":{"LABEL":"example"},"auto_start":false,"alwaysLoad":true,"startup_timeout_seconds":7,"call_timeout_seconds":9,"tool_timeout_seconds":{"list":3},"disabled_tools":["write"]}}`
		if wrapped {
			body = `{"mcpServers":` + body + `}`
		}
		got, err := ParseMCPServersJSON([]byte(body))
		if err != nil {
			t.Fatal(err)
		}
		autoStart := false
		want := []PluginEntry{
			{Name: "local", Command: "example-mcp", Args: []string{"--stdio"}, Env: map[string]string{"LABEL": "example"}, AutoStart: &autoStart, Load: MCPLoadAlways, StartupTimeoutSeconds: 7, CallTimeoutSeconds: 9, ToolTimeoutSeconds: map[string]int{"list": 3}, DisabledTools: []string{"write"}},
			{Name: "remote", Type: "sse", URL: "https://example.com/mcp", Headers: map[string]string{"X-Example": "value"}},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("wrapped=%v: entries = %+v, want %+v", wrapped, got, want)
		}
	}
}

func TestLoadMCPJSONRetainsMetadataOnlyDefinitions(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), mcpJSONFile)
	if err := os.WriteFile(path, []byte(`{"mcpServers":{"local":{"env":{"LABEL":"example"},"auto_start":false}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := loadMCPJSON(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "local" || got[0].Env["LABEL"] != "example" || got[0].AutoStart == nil || *got[0].AutoStart || got[0].Source != MCPSourceProjectMCPJSON {
		t.Fatalf("loaded metadata-only definition = %+v", got)
	}
}
