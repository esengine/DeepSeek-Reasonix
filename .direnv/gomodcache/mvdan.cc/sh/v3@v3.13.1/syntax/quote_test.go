// Copyright (c) 2021, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import (
	"testing"

	"github.com/go-quicktest/qt"
)

func TestQuote(t *testing.T) {
	t.Parallel()
	tests := [...]struct {
		str  string
		lang LangVariant
		want any
	}{
		{"", LangBash, `''`},
		{"\a", LangBash, `$'\a'`},
		{"\b", LangBash, `$'\b'`},
		{"\f", LangBash, `$'\f'`},
		{"\n", LangBash, `$'\n'`},
		{"\r", LangBash, `$'\r'`},
		{"\t", LangBash, `$'\t'`},
		{"\v", LangBash, `$'\v'`},
		{"null\x00", LangBash, &QuoteError{4, quoteErrNull}},
		{"posix\x1b", LangPOSIX, &QuoteError{5, quoteErrPOSIX}},
		{"posix\n", LangPOSIX, &QuoteError{5, quoteErrPOSIX}},
		{"mksh16\U00086199", LangMirBSDKorn, &QuoteError{6, quoteErrMksh}},
		{"\x1b\x1caaa", LangBash, `$'\x1b\x1caaa'`},
		{"\x1b\x1caaa", LangMirBSDKorn, `$'\x1b\x1c'$'aaa'`},
		{"\xff\x00", LangBash, &QuoteError{1, quoteErrNull}},
	}

	for _, test := range tests {
		t.Run("", func(t *testing.T) {
			t.Parallel()

			got, gotErr := Quote(test.str, test.lang)
			switch want := test.want.(type) {
			case string:
				qt.Assert(t, qt.Equals(got, want))
				qt.Assert(t, qt.IsNil(gotErr))
			case *QuoteError:
				qt.Assert(t, qt.Equals(got, ""))
				qt.Assert(t, qt.DeepEquals(gotErr, error(want)))
			default:
				t.Fatalf("unexpected type: %T", want)
			}
		})
	}
}
