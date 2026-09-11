package outputstyle

import "testing"

// TestIsFalse covers the boolean-string parser used for
// keep-coding-instructions frontmatter: false/no/0/off are falsy,
// case-insensitively and with surrounding whitespace tolerated.
func TestIsFalse(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		// Falsy spellings.
		{in: "false", want: true},
		{in: "no", want: true},
		{in: "0", want: true},
		{in: "off", want: true},
		// Case-insensitive.
		{in: "FALSE", want: true},
		{in: "No", want: true},
		{in: "Off", want: true},
		{in: "fAlSe", want: true},
		// Surrounding whitespace is trimmed.
		{in: "  false  ", want: true},
		{in: "\tno\n", want: true},
		{in: " 0 ", want: true},
		// Truthy spellings are not false.
		{in: "true", want: false},
		{in: "yes", want: false},
		{in: "1", want: false},
		{in: "on", want: false},
		{in: "TRUE", want: false},
		{in: " ON ", want: false},
		// Empty and unknown values are not false (keep the default).
		{in: "", want: false},
		{in: "   ", want: false},
		{in: "maybe", want: false},
		{in: "falsey", want: false},
		{in: "0.0", want: false},
		{in: "nope", want: false},
	} {
		if got := isFalse(tc.in); got != tc.want {
			t.Errorf("isFalse(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
