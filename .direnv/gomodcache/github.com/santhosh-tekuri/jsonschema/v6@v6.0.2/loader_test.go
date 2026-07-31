package jsonschema

import (
	"strings"
	"testing"
)

func TestUnmarshalJSON(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"{", false},
		{"{}", true},
		{"{}A", false},
		{"{}{}", false},
	}

	for _, test := range tests {
		_, err := UnmarshalJSON(strings.NewReader(test.input))
		if valid := err == nil; valid != test.valid {
			if err != nil {
				t.Log(err)
				t.Errorf("UnmarshalJSON(%q) valid: got %v, want %v", test.input, valid, test.valid)
			}
		}
	}
}
