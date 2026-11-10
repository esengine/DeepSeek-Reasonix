package shellparse

import (
	"reflect"
	"testing"
)

func TestDeleteLiteralEscapes(t *testing.T) {
	for _, tc := range []struct {
		input  string
		quoted bool
		want   string
	}{
		{`child`, false, `child`}, {`child\`, false, `child\`},
		{`child\ name`, false, `child name`}, {"child\\\nname", false, "childname"},
		{`child\q`, true, `child\q`}, {`child\$`, true, `child$`},
		{"child\\`", true, "child`"}, {`child\"`, true, `child"`}, {`child\\`, true, `child\`},
	} {
		if got := unescapeLit(tc.input, tc.quoted); got != tc.want {
			t.Fatalf("literal %q (quoted %v) = %q, want %q", tc.input, tc.quoted, got, tc.want)
		}
	}
	for _, tc := range []struct {
		source string
		want   []string
	}{
		{`rm -rf child\ name`, []string{"-rf", "child name"}},
		{`rm -rf "child\q"`, []string{"-rf", `child\q`}},
	} {
		got, err := AnalyzeDeleteCalls(tc.source)
		if err != nil || len(got.Calls) != 1 || !reflect.DeepEqual(got.Calls[0].Args, tc.want) {
			t.Fatalf("%q = %#v, %v", tc.source, got, err)
		}
	}
}

func TestDeleteAssignmentSyntax(t *testing.T) {
	for _, tc := range []struct {
		word string
		want bool
	}{
		{"x=1", true}, {"_=1", true}, {"a_b2=value", true},
		{"NAME=", true}, {"x", false}, {"=1", false},
		{"1x=1", false}, {"a-b=1", false}, {"a.b=1", false},
	} {
		t.Run(tc.word, func(t *testing.T) {
			if got := IsAssignment(tc.word); got != tc.want {
				t.Fatalf("assignment = %v, want %v", got, tc.want)
			}
		})
	}
}
