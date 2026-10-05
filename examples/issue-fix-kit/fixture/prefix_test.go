package prefix

import (
	"slices"
	"testing"
)

func TestTake(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values []int
		limit  int
		want   []int
	}{
		{name: "normal", values: []int{1, 2, 3}, limit: 2, want: []int{1, 2}},
		{name: "zero", values: []int{1, 2, 3}, limit: 0},
		{name: "oversized", values: []int{1, 2, 3}, limit: 5, want: []int{1, 2, 3}},
		{name: "negative", values: []int{1, 2, 3}, limit: -1},
		{name: "nil", limit: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := slices.Clone(tc.values)
			if got := Take(tc.values, tc.limit); !slices.Equal(got, tc.want) {
				t.Fatalf("Take(%v, %d) = %v, want %v", tc.values, tc.limit, got, tc.want)
			}
			if !slices.Equal(tc.values, before) {
				t.Fatalf("input changed: got %v, want %v", tc.values, before)
			}
		})
	}
}
