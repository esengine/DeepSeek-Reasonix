package builtin

import (
	"encoding/json"
	"testing"

	"reasonix/internal/contract/tool"
)

func TestExternalToolsDeclareWhereTheirContentCameFrom(t *testing.T) {
	cases := []struct {
		name string
		tool tool.Tool
		args string
		want tool.Provenance
	}{
		{"web_fetch", webFetch{}, `{"url":"https://docs.example.com/page"}`, tool.Provenance{Kind: tool.ProvenanceWeb, Source: "docs.example.com"}},
		{"browser_open with url", browserOpen{}, `{"url":"http://intra.corp:8080/"}`, tool.Provenance{Kind: tool.ProvenanceBrowser, Source: "intra.corp"}},
		{"browser_read without session", browserRead{}, `{}`, tool.Provenance{Kind: tool.ProvenanceBrowser}},
		{"computer_read", computerRead{}, `{}`, tool.Provenance{Kind: tool.ProvenanceDesktop}},
		{"read_file", readFile{}, `{"path":"a.go"}`, tool.Provenance{}},
	}
	for _, c := range cases {
		if got := tool.ProvenanceOf(c.tool, json.RawMessage(c.args)); got != c.want {
			t.Fatalf("%s: provenance = %+v, want %+v", c.name, got, c.want)
		}
	}
}
