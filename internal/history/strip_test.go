package history

import "testing"

// TestStripComposePrefixes covers the compose-block stripper used before
// indexing/rendering user messages: <memory-update>, <background-jobs>,
// <active-goal>, and <hook-context> blocks are removed from the start of the
// content (repeatedly, for stacked blocks), and anything after a leading
// "[Plan mode ...]" marker is kept.
func TestStripComposePrefixes(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "plain text", in: "hello world", want: "hello world"},
		{name: "plain text with whitespace", in: "  hello world\n", want: "hello world"},
		{name: "memory-update block", in: "<memory-update>\ncontext\n</memory-update>\nreal content", want: "real content"},
		{name: "background-jobs block", in: "<background-jobs>\njob list\n</background-jobs>\nreal content", want: "real content"},
		{name: "active-goal block", in: "<active-goal>\ngoal\n</active-goal>\nreal content", want: "real content"},
		{name: "hook-context block", in: "<hook-context>\nctx\n</hook-context>\nreal content", want: "real content"},
		{name: "block with attributes", in: "<memory-update id=\"42\">\nctx\n</memory-update>\nreal content", want: "real content"},
		{name: "single-line block", in: "<memory-update>x</memory-update>\nreal", want: "real"},
		{name: "multiple stacked blocks", in: "<memory-update>\na\n</memory-update>\n<background-jobs>\nb\n</background-jobs>\nreal content", want: "real content"},
		{name: "plan mode marker", in: "[Plan mode] real content", want: "real content"},
		{name: "plan mode marker with label", in: "[Plan mode: active] plan notes", want: "plan notes"},
		{name: "blocks then plan mode marker", in: "<memory-update>\nctx\n</memory-update>\n[Plan mode] real content", want: "real content"},
		{name: "leftover after marker", in: "[Plan mode]\n\n  leftover here  ", want: "leftover here"},
		{name: "non-prefix block is kept", in: "intro <memory-update>x</memory-update>\nreal", want: "intro <memory-update>x</memory-update>\nreal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := stripComposePrefixes(tc.in); got != tc.want {
				t.Errorf("stripComposePrefixes(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
