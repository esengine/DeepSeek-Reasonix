package agentcomposition

import (
	"encoding/json"
	"os"
	"testing"
)

func TestStudioMockBuiltinRevisionsMatchKernel(t *testing.T) {
	data, err := os.ReadFile("builtin-revisions.json")
	if err != nil {
		t.Fatal(err)
	}
	var revisions map[string]string
	if err := json.Unmarshal(data, &revisions); err != nil {
		t.Fatal(err)
	}
	for _, d := range Builtins() {
		if revisions[d.ID] != Revision(d) {
			t.Fatalf("refresh Studio Mock revision for %s", d.ID)
		}
	}
}
