package capability

import "testing"

// The id a model reaches for is often the tool's own name: that is what every
// other description, error message and skill body calls it. A catalog that
// answers "unknown capability_id" for a capability it is holding sends the
// model looking for a workaround it does not need.
func TestLookupAcceptsAToolsOwnName(t *testing.T) {
	c := Catalog{Entries: []Entry{
		{ID: "tool:parallel_tasks", Kind: KindTool, ToolName: "parallel_tasks", Aliases: []string{"task:parallel"}},
		{ID: "tool:grep", Kind: KindTool, ToolName: "grep"},
	}}
	for _, id := range []string{"tool:parallel_tasks", "task:parallel", "parallel_tasks"} {
		e, ok := c.Lookup(id)
		if !ok || e.ToolName != "parallel_tasks" {
			t.Errorf("Lookup(%q) = %+v, %v; want the parallel_tasks entry", id, e, ok)
		}
	}
}

// Guessing between two tools that answer to one name would route a call
// somewhere the model did not ask for, which is worse than saying no.
func TestLookupRefusesAnAmbiguousToolName(t *testing.T) {
	c := Catalog{Entries: []Entry{
		{ID: "tool:a/run", Kind: KindTool, ToolName: "run"},
		{ID: "tool:b/run", Kind: KindTool, ToolName: "run"},
	}}
	if e, ok := c.Lookup("run"); ok {
		t.Fatalf("Lookup(\"run\") resolved to %+v; two tools answer to it", e)
	}
}
