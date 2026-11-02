package delegation

import (
	"slices"
	"testing"

	"reasonix/internal/contract/provider"
)

func TestTouchedPathsListsEachReadAndWrittenFileOnce(t *testing.T) {
	call := func(name, args string) provider.Message {
		return provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{Name: name, Arguments: args}}}
	}
	read, wrote := touchedPaths([]provider.Message{
		call("read_file", `{"path":"a.go"}`),
		call("read_file", `{"path":"a.go"}`),
		call("read_file", `{"path":"b.go"}`),
		call("write_file", `{"path":"c.go","content":"x"}`),
		call("bash", `{"command":"ls"}`),
		call("read_file", `not json`),
	})
	if !slices.Equal(read, []string{"a.go", "b.go"}) || !slices.Equal(wrote, []string{"c.go"}) {
		t.Fatalf("read=%v wrote=%v, want [a.go b.go] and [c.go]", read, wrote)
	}
}
