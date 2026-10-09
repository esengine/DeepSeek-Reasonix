package shellparse

import (
	"reflect"
	"testing"
)

func TestAnalyzeDeleteCallsStructure(t *testing.T) {
	unsafe := DeleteCall{Unsafe: true}
	rm := func(unsafe bool) DeleteCall {
		return DeleteCall{Name: "rm", Args: []string{"-rf", "x"}, Unsafe: unsafe}
	}
	for _, tc := range []struct {
		source string
		calls  []DeleteCall
		err    bool
	}{
		{`rm -rf '`, nil, true},
		{`if true; then`, nil, true},
		{``, nil, false},
		{`rm -rf x`, []DeleteCall{rm(false)}, false},
		{`rm -rf x &`, []DeleteCall{rm(true)}, false},
		{`! rm -rf x`, []DeleteCall{rm(true)}, false},
		{`(rm -rf x)`, []DeleteCall{unsafe, rm(true)}, false},
		{`if true; then rm -rf x; fi`, []DeleteCall{unsafe, {Name: "true", Unsafe: true}, rm(true)}, false},
		{`while false; do :; done`, []DeleteCall{unsafe, {Name: "false", Unsafe: true}, {Name: ":", Unsafe: true}}, false},
		{`((x=1))`, []DeleteCall{unsafe}, false},
		{`f(){ :; }`, []DeleteCall{unsafe, unsafe, {Name: ":", Unsafe: true}}, false},
		{`x=1`, []DeleteCall{unsafe}, false},
		{`>out`, nil, false},
		{`echo "$(rm -rf x)"`, []DeleteCall{{Name: "echo", Args: []string{""}}, rm(true)}, false},
		{`echo "$(echo "$(rm -rf x)")"`, []DeleteCall{{Name: "echo", Args: []string{""}}, {Name: "echo", Args: []string{""}, Unsafe: true}, rm(true)}, false},
		{`x=1 rm -rf x`, []DeleteCall{rm(true)}, false},
		{`rm -rf x && make`, []DeleteCall{rm(false), {Name: "make"}}, false},
		{`rm -rf x || true`, []DeleteCall{rm(false), {Name: "true"}}, false},
		{`rm -rf x | cat`, []DeleteCall{rm(true), {Name: "cat", Unsafe: true}}, false},
		{`cd app; rm -rf x 2>/dev/null`, []DeleteCall{{Name: "cd", Args: []string{"app"}}, rm(false)}, false},
		{`rm -rf "$target"`, []DeleteCall{{Name: "rm", Args: []string{"-rf", ""}}}, false},
	} {
		t.Run(tc.source, func(t *testing.T) {
			got, err := AnalyzeDeleteCalls(tc.source)
			if (err != nil) != tc.err {
				t.Fatalf("parse error = %v, want error %v", err, tc.err)
			}
			want := DeleteAnalysis{Standalone: !tc.err, Calls: tc.calls}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("analysis = %#v, want %#v", got, want)
			}
		})
	}
}
