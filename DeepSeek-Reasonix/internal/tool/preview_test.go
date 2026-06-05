package tool

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"reasonix/internal/diff"
)

type fakeWriter struct {
	readOnly bool
	change   diff.Change
	err      error
}

func (f fakeWriter) Name() string                                             { return "fake" }
func (f fakeWriter) Description() string                                      { return "fake" }
func (f fakeWriter) Schema() json.RawMessage                                  { return json.RawMessage(`{}`) }
func (f fakeWriter) Execute(context.Context, json.RawMessage) (string, error) { return "", nil }
func (f fakeWriter) ReadOnly() bool                                           { return f.readOnly }
func (f fakeWriter) Preview(json.RawMessage) (diff.Change, error)             { return f.change, f.err }

type plainWriter struct{}

func (plainWriter) Name() string                                             { return "plain" }
func (plainWriter) Description() string                                      { return "plain" }
func (plainWriter) Schema() json.RawMessage                                  { return json.RawMessage(`{}`) }
func (plainWriter) Execute(context.Context, json.RawMessage) (string, error) { return "", nil }
func (plainWriter) ReadOnly() bool                                           { return false }

func TestPreviewChange(t *testing.T) {
	good := diff.Change{Diff: "@@\n+a\n", Added: 1}
	cases := []struct {
		name string
		tool Tool
		want bool
	}{
		{"nil tool", nil, false},
		{"read-only skipped", fakeWriter{readOnly: true, change: good}, false},
		{"writer without previewer", plainWriter{}, false},
		{"preview error", fakeWriter{err: errors.New("boom")}, false},
		{"binary skipped", fakeWriter{change: diff.Change{Binary: true}}, false},
		{"textual change", fakeWriter{change: good}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ch, ok := PreviewChange(c.tool, json.RawMessage(`{}`))
			if ok != c.want {
				t.Fatalf("ok = %v, want %v", ok, c.want)
			}
			if ok && ch.Diff == "" {
				t.Fatal("expected a non-empty diff on success")
			}
		})
	}
}
