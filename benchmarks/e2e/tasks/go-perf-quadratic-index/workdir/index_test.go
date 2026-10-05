package index

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestIndex(t *testing.T) {
	got := Index([]string{"a b a", "b c", "a"})
	want := map[string][]int{"a": {0, 2}, "b": {0, 1}, "c": {1}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestIndexIsFastEnough(t *testing.T) {
	lines := make([]string, 40000)
	for i := range lines {
		lines[i] = fmt.Sprintf("the common word number %d", i)
	}
	start := time.Now()
	got := Index(lines)
	if elapsed := time.Since(start); elapsed > 300*time.Millisecond {
		t.Fatalf("Index took %v, want under 300ms", elapsed)
	}
	if len(got["the"]) != len(lines) {
		t.Fatalf("the appears on %d lines, want %d", len(got["the"]), len(lines))
	}
}
