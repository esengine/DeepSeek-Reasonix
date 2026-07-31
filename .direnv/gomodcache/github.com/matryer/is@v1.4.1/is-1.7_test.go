// +build go1.7

package is

import (
	"bytes"
	"strings"
	"testing"
)

// TestSubtests ensures subtests work as expected.
// https://github.com/matryer/is/issues/1
func TestSubtests(t *testing.T) {
	t.Run("sub1", func(t *testing.T) {
		is := New(t)
		is.Equal(1+1, 2)
	})
}

func TestHelper(t *testing.T) {
	tests := []struct {
		name             string
		helper           bool
		expectedFilename string
	}{
		{
			name:             "without helper",
			helper:           false,
			expectedFilename: "is_helper_test.go",
		},
		{
			name:             "with helper",
			helper:           true,
			expectedFilename: "is-1.7_test.go",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tt := &mockT{}
			is := NewRelaxed(tt)

			var buf bytes.Buffer
			is.out = &buf
			helper(is, tc.helper)
			actual := buf.String()
			t.Log(actual)
			if !strings.Contains(actual, tc.expectedFilename) {
				t.Errorf("string does not contain correct filename: %s", actual)
			}
		})
	}
}
