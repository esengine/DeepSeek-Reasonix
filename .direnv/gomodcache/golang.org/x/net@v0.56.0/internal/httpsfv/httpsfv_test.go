// Copyright 2025 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
package httpsfv

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParseList(t *testing.T) {
	tests := []struct {
		name        string
		in          string
		wantMembers []string
		wantParams  []string
		wantOk      bool
	}{
		{
			name:        "valid list",
			in:          `a, b,c`,
			wantMembers: []string{"a", "b", "c"},
			wantParams:  []string{"", "", ""},
			wantOk:      true,
		},
		{
			name:        "valid list with params",
			in:          `a;foo=bar, b,c; baz=baz`,
			wantMembers: []string{"a", "b", "c"},
			wantParams:  []string{";foo=bar", "", "; baz=baz"},
			wantOk:      true,
		},
		{
			name:        "valid list with fake commas",
			in:          `a;foo=",", (",")`,
			wantMembers: []string{"a", `(",")`},
			wantParams:  []string{`;foo=","`, ""},
			wantOk:      true,
		},
		{
			name:        "valid list with inner list member",
			in:          `(a b c); foo, bar;baz`,
			wantMembers: []string{"(a b c)", "bar"},
			wantParams:  []string{"; foo", ";baz"},
			wantOk:      true,
		},
		{
			name:        "invalid list with trailing comma",
			in:          `a;foo=bar, b,c; baz=baz,`,
			wantMembers: []string{"a", "b", "c"},
			wantParams:  []string{";foo=bar", "", "; baz=baz"},
		},
		{
			name: "invalid list with unclosed string",
			in:   `", b, c,d`,
		},
	}

	for _, tc := range tests {
		var gotMembers, gotParams []string
		f := func(member, param string) {
			gotMembers = append(gotMembers, member)
			gotParams = append(gotParams, param)
		}
		ok := ParseList(tc.in, f)
		if ok != tc.wantOk {
			t.Fatalf("test %q: want ok to be %v, got: %v", tc.name, tc.wantOk, ok)
		}
		if !slices.Equal(tc.wantMembers, gotMembers) {
			t.Fatalf("test %q: mismatch.\n got: %#v\nwant: %#v\n", tc.name, gotMembers, tc.wantMembers)
		}
		if !slices.Equal(tc.wantParams, gotParams) {
			t.Fatalf("test %q: mismatch.\n got: %#v\nwant: %#v\n", tc.name, gotParams, tc.wantParams)
		}
	}
}

func TestConsumeBareInnerList(t *testing.T) {
	tests := []struct {
		name          string
		in            string
		wantBareItems []string
		wantParams    []string
		wantListParam string
		wantOk        bool
	}{
		{
			name:          "valid inner list without param",
			in:            `(a b c)`,
			wantBareItems: []string{"a", "b", "c"},
			wantParams:    []string{"", "", ""},
			wantOk:        true,
		},
		{
			name:          "valid inner list with param",
			in:            `(a;d b c;e)`,
			wantBareItems: []string{"a", "b", "c"},
			wantParams:    []string{";d", "", ";e"},
			wantOk:        true,
		},
		{
			name:          "valid inner list with fake ending parenthesis",
			in:            `(")";foo=")")`,
			wantBareItems: []string{`")"`},
			wantParams:    []string{`;foo=")"`},
			wantOk:        true,
		},
		{
			name:          "valid inner list with list parameter",
			in:            `(a b;c); d`,
			wantBareItems: []string{"a", "b"},
			wantParams:    []string{"", ";c"},
			wantOk:        true,
		},
		{
			name:          "valid inner list with more content after",
			in:            `(a b;c); d, a`,
			wantBareItems: []string{"a", "b"},
			wantParams:    []string{"", ";c"},
			wantOk:        true,
		},
		{
			name:          "invalid inner list",
			in:            `(a b;c `,
			wantBareItems: []string{"a", "b"},
			wantParams:    []string{"", ";c"},
		},
	}

	for _, tc := range tests {
		var gotBareItems, gotParams []string
		f := func(bareItem, param string) {
			gotBareItems = append(gotBareItems, bareItem)
			gotParams = append(gotParams, param)
		}
		gotConsumed, gotRest, ok := consumeBareInnerList(tc.in, f)
		if ok != tc.wantOk {
			t.Fatalf("test %q: want ok to be %v, got: %v", tc.name, tc.wantOk, ok)
		}
		if !slices.Equal(tc.wantBareItems, gotBareItems) {
			t.Fatalf("test %q: mismatch.\n got: %#v\nwant: %#v\n", tc.name, gotBareItems, tc.wantBareItems)
		}
		if !slices.Equal(tc.wantParams, gotParams) {
			t.Fatalf("test %q: mismatch.\n got: %#v\nwant: %#v\n", tc.name, gotParams, tc.wantParams)
		}
		if gotConsumed+gotRest != tc.in {
			t.Fatalf("test %q: %#v + %#v != %#v", tc.name, gotConsumed, gotRest, tc.in)
		}
	}
}

func TestParseBareInnerList(t *testing.T) {
	tests := []struct {
		name          string
		in            string
		wantBareItems []string
		wantParams    []string
		wantOk        bool
	}{
		{
			name:          "valid inner list",
			in:            `(a b;c)`,
			wantBareItems: []string{"a", "b"},
			wantParams:    []string{"", ";c"},
			wantOk:        true,
		},
		{
			name:          "valid inner list with list parameter",
			in:            `(a b;c); d`,
			wantBareItems: []string{"a", "b"},
			wantParams:    []string{"", ";c"},
		},
		{
			name:          "invalid inner list",
			in:            `(a b;c `,
			wantBareItems: []string{"a", "b"},
			wantParams:    []string{"", ";c"},
		},
	}

	for _, tc := range tests {
		var gotBareItems, gotParams []string
		f := func(bareItem, param string) {
			gotBareItems = append(gotBareItems, bareItem)
			gotParams = append(gotParams, param)
		}
		ok := ParseBareInnerList(tc.in, f)
		if ok != tc.wantOk {
			t.Fatalf("test %q: want ok to be %v, got: %v", tc.name, tc.wantOk, ok)
		}
		if !slices.Equal(tc.wantBareItems, gotBareItems) {
			t.Fatalf("test %q: mismatch.\n got: %#v\nwant: %#v\n", tc.name, gotBareItems, tc.wantBareItems)
		}
		if !slices.Equal(tc.wantParams, gotParams) {
			t.Fatalf("test %q: mismatch.\n got: %#v\nwant: %#v\n", tc.name, gotParams, tc.wantParams)
		}
	}
}

func TestConsumeItem(t *testing.T) {
	tests := []struct {
		name         string
		in           string
		wantBareItem string
		wantParam    string
		wantOk       bool
	}{
		{
			name:         "valid bare item",
			in:           `fookey`,
			wantBareItem: `fookey`,
			wantOk:       true,
		},
		{
			name:         "valid bare item and param",
			in:           `fookey; a="123"`,
			wantBareItem: `fookey`,
			wantParam:    `; a="123"`,
			wantOk:       true,
		},
		{
			name:         "valid item with content after",
			in:           `fookey; a="123", otheritem; otherparam=1`,
			wantBareItem: `fookey`,
			wantParam:    `; a="123"`,
			wantOk:       true,
		},
		{
			name: "invalid just param",
			in:   `;a="123"`,
		},
	}

	for _, tc := range tests {
		var gotBareItem, gotParam string
		f := func(bareItem, param string) {
			gotBareItem = bareItem
			gotParam = param
		}
		gotConsumed, gotRest, ok := consumeItem(tc.in, f)
		if ok != tc.wantOk {
			t.Fatalf("test %q: want ok to be %v, got: %v", tc.name, tc.wantOk, ok)
		}
		if tc.wantBareItem != gotBareItem {
			t.Fatalf("test %q: mismatch.\n got: %#v\nwant: %#v\n", tc.name, gotBareItem, tc.wantBareItem)
		}
		if tc.wantParam != gotParam {
			t.Fatalf("test %q: mismatch.\n got: %#v\nwant: %#v\n", tc.name, gotParam, tc.wantParam)
		}
		if gotConsumed+gotRest != tc.in {
			t.Fatalf("test %q: %#v + %#v != %#v", tc.name, gotConsumed, gotRest, tc.in)
		}
	}
}

func TestParseItem(t *testing.T) {
	tests := []struct {
		name         string
		in           string
		wantBareItem string
		wantParam    string
		wantOk       bool
	}{
		{
			name:         "valid bare item",
			in:           `fookey`,
			wantBareItem: `fookey`,
			wantOk:       true,
		},
		{
			name:         "valid bare item and param",
			in:           `fookey; a="123"`,
			wantBareItem: `fookey`,
			wantParam:    `; a="123"`,
			wantOk:       true,
		},
		{
			name:         "valid item with content after",
			in:           `fookey; a="123", otheritem; otherparam=1`,
			wantBareItem: `fookey`,
			wantParam:    `; a="123"`,
		},
		{
			name: "invalid just param",
			in:   `;a="123"`,
		},
	}

	for _, tc := range tests {
		var gotBareItem, gotParam string
		f := func(bareItem, param string) {
			gotBareItem = bareItem
			gotParam = param
		}
		ok := ParseItem(tc.in, f)
		if ok != tc.wantOk {
			t.Fatalf("test %q: want ok to be %v, got: %v", tc.name, tc.wantOk, ok)
		}
		if tc.wantBareItem != gotBareItem {
			t.Fatalf("test %q: mismatch.\n got: %#v\nwant: %#v\n", tc.name, gotBareItem, tc.wantBareItem)
		}
		if tc.wantParam != gotParam {
			t.Fatalf("test %q: mismatch.\n got: %#v\nwant: %#v\n", tc.name, gotParam, tc.wantParam)
		}
	}
}

func TestParseDictionary(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		wantVal   string
		wantParam string
		wantOk    bool
	}{
		{
			name:    "valid dictionary with simple value",
			in:      `a=b, want=foo, c=d`,
			wantVal: "foo",
			wantOk:  true,
		},
		{
			name:    "valid dictionary with implicit value",
			in:      `a, want, c=d`,
			wantVal: "?1",
			wantOk:  true,
		},
		{
			name:      "valid dictionary with parameter",
			in:        `a, want=foo;bar=baz, c=d`,
			wantVal:   "foo",
			wantParam: ";bar=baz",
			wantOk:    true,
		},
		{
			name:      "valid dictionary with inner list",
			in:        `a, want=(a b c d;e;f);g=h, c=d`,
			wantVal:   "(a b c d;e;f)",
			wantParam: ";g=h",
			wantOk:    true,
		},
		{
			name:      "valid dictionary with fake commas",
			in:        `a=(";");b=";",want=foo;bar`,
			wantVal:   "foo",
			wantParam: ";bar",
			wantOk:    true,
		},
		{
			name: "invalid dictionary with bad key",
			in:   `UPPERCASEKEY=BAD, want=foo, c=d`,
		},
		{
			name: "invalid dictionary with trailing comma",
			in:   `trailing=comma,`,
		},
		{
			name: "invalid dictionary with unclosed string",
			in:   `a=""",want=foo;bar`,
		},
	}

	for _, tc := range tests {
		var gotVal, gotParam string
		f := func(key, val, param string) {
			if key == "want" {
				gotVal = val
				gotParam = param
			}
		}
		ok := ParseDictionary(tc.in, f)
		if ok != tc.wantOk {
			t.Fatalf("test %q: want ok to be %v, got: %v", tc.name, tc.wantOk, ok)
		}
		if tc.wantVal != gotVal {
			t.Fatalf("test %q: mismatch.\n got: %#v\nwant: %#v\n", tc.name, gotVal, tc.wantVal)
		}
		if tc.wantParam != gotParam {
			t.Fatalf("test %q: mismatch.\n got: %#v\nwant: %#v\n", tc.name, gotParam, tc.wantParam)
		}
	}
}

func TestConsumeParameter(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		want   any
		wantOk bool
	}{
		{
			name:   "valid string",
			in:     `;parameter;want="wantvalue"`,
			want:   "wantvalue",
			wantOk: true,
		},
		{
			name:   "valid integer",
			in:     `;parameter;want=123456;something`,
			want:   123456,
			wantOk: true,
		},
		{
			name:   "valid decimal",
			in:     `;parameter;want=3.14;something`,
			want:   3.14,
			wantOk: true,
		},
		{
			name:   "valid implicit bool",
			in:     `;parameter;want;something`,
			want:   true,
			wantOk: true,
		},
		{
			name:   "valid token",
			in:     `;want=*atoken;something`,
			want:   "*atoken",
			wantOk: true,
		},
		{
			name:   "valid byte sequence",
			in:     `;want=:eWF5Cg==:;something`,
			want:   "eWF5Cg==",
			wantOk: true,
		},
		{
			name:   "valid repeated key",
			in:     `;want=:eWF5Cg==:;now;want=1;is;repeated;want="overwritten!"`,
			want:   "overwritten!",
			wantOk: true,
		},
		{
			name:   "valid parameter with content after",
			in:     `;want=:eWF5Cg==:;now;want=1;is;repeated;want="overwritten!", some=stuff`,
			want:   "overwritten!",
			wantOk: true,
		},
		{
			name: "invalid parameter",
			in:   `;UPPERCASEKEY=NOT_ACCEPTED`,
		},
	}

	for _, tc := range tests[len(tests)-1:] {
		var got any
		f := func(key, val string) {
			if key != "want" {
				return
			}
			switch {
			case strings.HasPrefix(val, "?"): // Bool
				got = val == "?1"
			case strings.HasPrefix(val, `"`): // String
				got = val[1 : len(val)-1]
			case strings.HasPrefix(val, "*"): // Token
				got = val
			case strings.HasPrefix(val, ":"): // Byte sequence
				got = val[1 : len(val)-1]
			default:
				if valConv, err := strconv.Atoi(val); err == nil { // Integer
					got = valConv
					return
				}
				if valConv, err := strconv.ParseFloat(val, 64); err == nil { // Float
					got = valConv
					return
				}
			}
		}
		consumed, rest, ok := consumeParameter(tc.in, f)
		if ok != tc.wantOk {
			t.Fatalf("test %q: want ok to be %v, got: %v", tc.name, tc.wantOk, ok)
		}
		if got != tc.want {
			t.Fatalf("test %q: mismatch.\n got: %#v\nwant: %#v\n", tc.name, got, tc.want)
		}
		if consumed+rest != tc.in {
			t.Fatalf("test %q: %#v + %#v != %#v", tc.name, got, rest, tc.in)
		}
	}
}

func TestParseParameter(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		want   any
		wantOk bool
	}{
		{
			name:   "valid parameter",
			in:     `;parameter;want="wantvalue"`,
			want:   "wantvalue",
			wantOk: true,
		},
		{
			name: "valid parameter with content after",
			in:   `;want=:eWF5Cg==:;now;want=1;is;repeated;want="overwritten!", some=stuff`,
			want: "overwritten!",
		},
		{
			name: "invalid parameter",
			in:   `;UPPERCASEKEY=NOT_ACCEPTED`,
		},
	}

	for _, tc := range tests[len(tests)-1:] {
		var got any
		f := func(key, val string) {
			if key != "want" {
				return
			}
			switch {
			case strings.HasPrefix(val, "?"): // Bool
				got = val == "?1"
			case strings.HasPrefix(val, `"`): // String
				got = val[1 : len(val)-1]
			case strings.HasPrefix(val, "*"): // Token
				got = val
			case strings.HasPrefix(val, ":"): // Byte sequence
				got = val[1 : len(val)-1]
			default:
				if valConv, err := strconv.Atoi(val); err == nil { // Integer
					got = valConv
					return
				}
				if valConv, err := strconv.ParseFloat(val, 64); err == nil { // Float
					got = valConv
					return
				}
			}
		}
		ok := ParseParameter(tc.in, f)
		if ok != tc.wantOk {
			t.Fatalf("test %q: want ok to be %v, got: %v", tc.name, tc.wantOk, ok)
		}
		if got != tc.want {
			t.Fatalf("test %q: mismatch.\n got: %#v\nwant: %#v\n", tc.name, got, tc.want)
		}
	}
}

func TestConsumeKey(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		want   string
		wantOk bool
	}{
		{
			name:   "valid basic key",
			in:     `fookey`,
			want:   `fookey`,
			wantOk: true,
		},
		{
			name:   "valid basic key with more content after",
			in:     `fookey,u=7`,
			want:   `fookey`,
			wantOk: true,
		},
		{
			name: "invalid key",
			in:   `1keycannotstartwithnum`,
		},
	}

	for _, tc := range tests {
		got, gotRest, ok := consumeKey(tc.in)
		if ok != tc.wantOk {
			t.Fatalf("test %q: want ok to be %v, got: %v", tc.name, tc.wantOk, ok)
		}
		if tc.want != got {
			t.Fatalf("test %q: mismatch.\n got: %#v\nwant: %#v\n", tc.name, got, tc.want)
		}
		if got+gotRest != tc.in {
			t.Fatalf("test %q: %#v + %#v != %#v", tc.name, got, gotRest, tc.in)
		}
	}
}

func TestConsumeIntegerOrDecimal(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		want   string
		wantOk bool
	}{
		{
			name:   "valid integer",
			in:     "123456",
			want:   "123456",
			wantOk: true,
		},
		{
			name:   "valid integer with more content after",
			in:     "123456,12345",
			want:   "123456",
			wantOk: true,
		},
		{
			name:   "valid max integer",
			in:     "999999999999999",
			want:   "999999999999999",
			wantOk: true,
		},
		{
			name:   "valid min integer",
			in:     "-999999999999999",
			want:   "-999999999999999",
			wantOk: true,
		},
		{
			name: "invalid integer too high",
			in:   "9999999999999999",
		},
		{
			name: "invalid integer too low",
			in:   "-9999999999999999",
		},
		{
			name:   "valid decimal",
			in:     "-123456789012.123",
			want:   "-123456789012.123",
			wantOk: true,
		},
		{
			name: "invalid decimal integer component too long",
			in:   "1234567890123.1",
		},
		{
			name: "invalid decimal fraction component too long",
			in:   "1.1234",
		},
		{
			name: "invalid decimal trailing dot",
			in:   "1.",
		},
	}

	for _, tc := range tests {
		got, gotRest, ok := consumeIntegerOrDecimal(tc.in)
		if ok != tc.wantOk {
			t.Fatalf("test %q: want ok to be %v, got: %v", tc.name, tc.wantOk, ok)
		}
		if tc.want != got {
			t.Fatalf("test %q: mismatch.\n got: %#v\nwant: %#v\n", tc.name, got, tc.want)
		}
		if got+gotRest != tc.in {
			t.Fatalf("test %q: %#v + %#v != %#v", tc.name, got, gotRest, tc.in)
		}
	}
}

func TestParseInteger(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		want   int64
		wantOk bool
	}{
		{
			name:   "valid integer",
			in:     "123456",
			want:   123456,
			wantOk: true,
		},
		{
			name: "valid integer with more content after",
			in:   "123456,12345",
		},
		{
			name:   "valid max integer",
			in:     "999999999999999",
			want:   999999999999999,
			wantOk: true,
		},
		{
			name:   "valid min integer",
			in:     "-999999999999999",
			want:   -999999999999999,
			wantOk: true,
		},
		{
			name: "invalid integer too high",
			in:   "9999999999999999",
		},
		{
			name: "invalid integer too low",
			in:   "-9999999999999999",
		},
		{
			name: "invalid integer with fraction",
			in:   "-123456789012.123",
		},
	}

	for _, tc := range tests {
		got, ok := ParseInteger(tc.in)
		if ok != tc.wantOk {
			t.Fatalf("test %q: want ok to be %v, got: %v", tc.name, tc.wantOk, ok)
		}
		if tc.want != got {
			t.Fatalf("test %q: mismatch.\n got: %#v\nwant: %#v\n", tc.name, got, tc.want)
		}
	}
}

func TestParseDecimal(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		want   float64
		wantOk bool
	}{
		{
			name:   "valid decimal",
			in:     "123456.789",
			want:   123456.789,
			wantOk: true,
		},
		{
			name: "valid decimal with more content after",
			in:   "123456.789, 123",
		},
		{
			name: "invalid decimal with no fraction",
			in:   "123456",
		},
		{
			name: "invalid decimal integer component too long",
			in:   "1234567890123.1",
		},
		{
			name: "invalid decimal fraction component too long",
			in:   "1.1234",
		},
		{
			name: "invalid decimal trailing dot",
			in:   "1.",
		},
	}

	for _, tc := range tests {
		got, ok := ParseDecimal(tc.in)
		if ok != tc.wantOk {
			t.Fatalf("test %q: want ok to be %v, got: %v", tc.name, tc.wantOk, ok)
		}
		if tc.want != got {
			t.Fatalf("test %q: mismatch.\n got: %#v\nwant: %#v\n", tc.name, got, tc.want)
		}
	}
}

func TestConsumeString(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		want   string
		wantOk bool
	}{
		{
			name:   "valid basic string",
			in:     `"foo bar"`,
			want:   `"foo bar"`,
			wantOk: true,
		},
		{
			name:   "valid basic string with more content after",
			in:     `"foo bar", a=3`,
			want:   `"foo bar"`,
			wantOk: true,
		},
		{
			name:   "valid string with escaped dquote",
			in:     `"foo bar \""`,
			want:   `"foo bar \""`,
			wantOk: true,
		},
		{
			name: "invalid string no starting dquote",
			in:   `foo bar"`,
		},
		{
			name: "invalid string no closing dquote",
			in:   `"foo bar`,
		},
		{
			name: "invalid string invalid character",
			in:   string([]byte{'"', 0x00, '"'}),
		},
	}

	for _, tc := range tests {
		got, gotRest, ok := consumeString(tc.in)
		if ok != tc.wantOk {
			t.Fatalf("test %q: want ok to be %v, got: %v", tc.name, tc.wantOk, ok)
		}
		if tc.want != got {
			t.Fatalf("test %q: mismatch.\n got: %#v\nwant: %#v\n", tc.name, got, tc.want)
		}
		if got+gotRest != tc.in {
			t.Fatalf("test %q: %#v + %#v != %#v", tc.name, got, gotRest, tc.in)
		}
	}
}

func TestParseString(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		want   string
		wantOk bool
	}{
		{
			name:   "valid basic string",
			in:     `"foo bar"`,
			want:   "foo bar",
			wantOk: true,
		},
		{
			name: "valid basic string with more content after",
			in:   `"foo bar", a=3`,
		},
		{
			name:   "valid string with escaped dquote",
			in:     `"foo bar \""`,
			want:   `foo bar \"`,
			wantOk: true,
		},
		{
			name: "invalid string no starting dquote",
			in:   `foo bar"`,
		},
		{
			name: "invalid string no closing dquote",
			in:   `"foo bar`,
		},
		{
			name: "invalid string invalid character",
			in:   string([]byte{'"', 0x00, '"'}),
		},
	}

	for _, tc := range tests {
		got, ok := ParseString(tc.in)
		if ok != tc.wantOk {
			t.Fatalf("test %q: want ok to be %v, got: %v", tc.name, tc.wantOk, ok)
		}
		if tc.want != got {
			t.Fatalf("test %q: mismatch.\n got: %#v\nwant: %#v\n", tc.name, got, tc.want)
		}
	}
}

func TestConsumeToken(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		want   string
		wantOk bool
	}{
		{
			name:   "valid token",
			in:     "*atoken",
			want:   "*atoken",
			wantOk: true,
		},
		{
			name:   "valid token with more content after",
			in:     "*atoken something",
			want:   "*atoken",
			wantOk: true,
		},
		{
			name: "invalid token",
			in:   "0invalid",
		},
	}

	for _, tc := range tests {
		got, gotRest, ok := consumeToken(tc.in)
		if ok != tc.wantOk {
			t.Fatalf("test %q: want ok to be %v, got: %v", tc.name, tc.wantOk, ok)
		}
		if tc.want != got {
			t.Fatalf("test %q: mismatch.\n got: %#v\nwant: %#v\n", tc.name, got, tc.want)
		}
		if got+gotRest != tc.in {
			t.Fatalf("test %q: %#v + %#v != %#v", tc.name, got, gotRest, tc.in)
		}
	}
}

func TestParseToken(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		want   string
		wantOk bool
	}{
		{
			name:   "valid token",
			in:     "a_b-c.d3:f%00/*",
			want:   "a_b-c.d3:f%00/*",
			wantOk: true,
		},
		{
			name:   "valid token with uppercase",
			in:     "FOOBAR",
			want:   "FOOBAR",
			wantOk: true,
		},
		{
			name: "valid token with content after",
			in:   "FOOBAR, foobar",
		},
		{
			name: "invalid token",
			in:   "0invalid",
		},
	}

	for _, tc := range tests {
		got, ok := ParseToken(tc.in)
		if ok != tc.wantOk {
			t.Fatalf("test %q: want ok to be %v, got: %v", tc.name, tc.wantOk, ok)
		}
		if tc.want != got {
			t.Fatalf("test %q: mismatch.\n got: %#v\nwant: %#v\n", tc.name, got, tc.want)
		}
	}
}

func TestConsumeByteSequence(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		want   string
		wantOk bool
	}{
		{
			name:   "valid byte sequence",
			in:     ":aGVsbG8gd29ybGQ=:",
			want:   ":aGVsbG8gd29ybGQ=:",
			wantOk: true,
		},
		{
			name:   "valid byte sequence with more content after",
			in:     ":aGVsbG8gd29ybGQ=::aGVsbG8gd29ybGQ=:",
			want:   ":aGVsbG8gd29ybGQ=:",
			wantOk: true,
		},
		{
			name: "invalid byte sequence character",
			in:   ":-:",
		},
		{
			name: "invalid byte sequence opening",
			in:   "aGVsbG8gd29ybGQ=:",
		},
		{
			name: "invalid byte sequence closing",
			in:   ":aGVsbG8gd29ybGQ=",
		},
	}

	for _, tc := range tests {
		got, gotRest, ok := consumeByteSequence(tc.in)
		if ok != tc.wantOk {
			t.Fatalf("test %q: want ok to be %v, got: %v", tc.name, tc.wantOk, ok)
		}
		if tc.want != got {
			t.Fatalf("test %q: mismatch.\n got: %#v\nwant: %#v\n", tc.name, got, tc.want)
		}
		if got+gotRest != tc.in {
			t.Fatalf("test %q: %#v + %#v != %#v", tc.name, got, gotRest, tc.in)
		}
	}
}

func TestParseByteSequence(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		want   []byte
		wantOk bool
	}{
		{
			name:   "valid byte sequence",
			in:     ":aGVsbG8gd29ybGQ=:",
			want:   []byte("aGVsbG8gd29ybGQ="),
			wantOk: true,
		},
		{
			name: "valid byte sequence with more content after",
			in:   ":aGVsbG8gd29ybGQ=::aGVsbG8gd29ybGQ=:",
		},
		{
			name: "invalid byte sequence character",
			in:   ":-:",
		},
		{
			name: "invalid byte sequence opening",
			in:   "aGVsbG8gd29ybGQ=:",
		},
		{
			name: "invalid byte sequence closing",
			in:   ":aGVsbG8gd29ybGQ=",
		},
	}

	for _, tc := range tests {
		got, ok := ParseByteSequence(tc.in)
		if ok != tc.wantOk {
			t.Fatalf("test %q: want ok to be %v, got: %v", tc.name, tc.wantOk, ok)
		}
		if !slices.Equal(tc.want, got) {
			t.Fatalf("test %q: mismatch.\n got: %#v\nwant: %#v\n", tc.name, got, tc.want)
		}
	}
}

func TestConsumeBoolean(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		want   string
		wantOk bool
	}{
		{
			name:   "valid boolean",
			in:     "?0",
			want:   "?0",
			wantOk: true,
		},
		{
			name:   "valid boolean with more content after",
			in:     "?1, a=1",
			want:   "?1",
			wantOk: true,
		},
		{
			name: "invalid boolean",
			in:   "!2",
		},
	}

	for _, tc := range tests {
		got, gotRest, ok := consumeBoolean(tc.in)
		if ok != tc.wantOk {
			t.Fatalf("test %q: want ok to be %v, got: %v", tc.name, tc.wantOk, ok)
		}
		if tc.want != got {
			t.Fatalf("test %q: mismatch.\n got: %#v\nwant: %#v\n", tc.name, got, tc.want)
		}
		if got+gotRest != tc.in {
			t.Fatalf("test %q: %#v + %#v != %#v", tc.name, got, gotRest, tc.in)
		}
	}
}

func TestParseBoolean(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		want   bool
		wantOk bool
	}{
		{
			name:   "valid boolean false",
			in:     "?0",
			want:   false,
			wantOk: true,
		},
		{
			name:   "valid boolean true",
			in:     "?1",
			want:   true,
			wantOk: true,
		},
		{
			name: "valid boolean with more content after",
			in:   "?1, a=1",
		},
		{
			name: "invalid boolean",
			in:   "?2",
		},
	}

	for _, tc := range tests {
		got, ok := ParseBoolean(tc.in)
		if ok != tc.wantOk {
			t.Fatalf("test %q: want ok to be %v, got: %v", tc.name, tc.wantOk, ok)
		}
		if tc.want != got {
			t.Fatalf("test %q: mismatch.\n got: %#v\nwant: %#v\n", tc.name, got, tc.want)
		}
	}
}

func TestConsumeDate(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		want   string
		wantOk bool
	}{
		{
			name:   "valid zero date",
			in:     "@0",
			want:   "@0",
			wantOk: true,
		},
		{
			name:   "valid positive date",
			in:     "@1659578233",
			want:   "@1659578233",
			wantOk: true,
		},
		{
			name:   "valid negative date",
			in:     "@-1659578233",
			want:   "@-1659578233",
			wantOk: true,
		},
		{
			name:   "valid large date",
			in:     "@25340221440",
			want:   "@25340221440",
			wantOk: true,
		},
		{
			name:   "valid small date",
			in:     "@-62135596800",
			want:   "@-62135596800",
			wantOk: true,
		},
		{
			name: "invalid decimal date",
			in:   "@1.2",
		},
		{
			name:   "valid date with more content after",
			in:     "@1659578233, foo;bar",
			want:   "@1659578233",
			wantOk: true,
		},
	}

	for _, tc := range tests {
		got, gotRest, ok := consumeDate(tc.in)
		if ok != tc.wantOk {
			t.Fatalf("test %q: want ok to be %v, got: %v", tc.name, tc.wantOk, ok)
		}
		if tc.want != got {
			t.Fatalf("test %q: mismatch.\n got: %#v\nwant: %#v\n", tc.name, got, tc.want)
		}
		if got+gotRest != tc.in {
			t.Fatalf("test %q: %#v + %#v != %#v", tc.name, got, gotRest, tc.in)
		}
	}
}

func TestParseDate(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		want   time.Time
		wantOk bool
	}{
		{
			name:   "valid zero date",
			in:     "@0",
			want:   time.Unix(0, 0),
			wantOk: true,
		},
		{
			name:   "valid positive date",
			in:     "@1659578233",
			want:   time.Date(2022, 8, 4, 1, 57, 13, 0, time.UTC).Local(),
			wantOk: true,
		},
		{
			name:   "valid negative date",
			in:     "@-1659578233",
			want:   time.Date(1917, 5, 30, 22, 2, 47, 0, time.UTC).Local(),
			wantOk: true,
		},
		{
			name:   "valid max date required",
			in:     "@253402214400",
			want:   time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC).Local(),
			wantOk: true,
		},
		{
			name:   "valid min date required",
			in:     "@-62135596800",
			want:   time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC).Local(),
			wantOk: true,
		},
		{
			name: "invalid date with fraction",
			in:   "@0.123",
		},
		{
			name: "valid date with more content after",
			in:   "@0, @0",
		},
	}

	for _, tc := range tests {
		got, ok := ParseDate(tc.in)
		if ok != tc.wantOk {
			t.Fatalf("test %q: want ok to be %v, got: %v", tc.name, tc.wantOk, ok)
		}
		if tc.want != got {
			t.Fatalf("test %q: mismatch.\n got: %#v\nwant: %#v\n", tc.name, got, tc.want)
		}
	}
}

func TestConsumeDisplayString(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		want   string
		wantOk bool
	}{
		{
			name:   "valid ascii string",
			in:     "%\" !%22#$%25&'()*+,-./0123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_`abcdefghijklmnopqrstuvwxyz{|}~\"",
			want:   "%\" !%22#$%25&'()*+,-./0123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_`abcdefghijklmnopqrstuvwxyz{|}~\"",
			wantOk: true,
		},
		{
			name:   "valid lowercase non-ascii string",
			in:     `%"f%c3%bc%c3%bc"`,
			want:   `%"f%c3%bc%c3%bc"`,
			wantOk: true,
		},
		{
			name: "invalid uppercase non-ascii string",
			in:   `%"f%C3%BC%C3%BC"`,
		},
		{
			name: "invalid unquoted string",
			in:   "%foo",
		},
		{
			name: "invalid string missing initial quote",
			in:   `%foo"`,
		},
		{
			name: "invalid string missing closing quote",
			in:   `%"foo`,
		},
		{
			name: "invalid tab in string",
			in:   "%\"\t\"",
		},
		{
			name: "invalid newline in string",
			in:   "%\"\n\"",
		},
		{
			name: "invalid single quoted string",
			in:   `%'foo'`,
		},
		{
			name: "invalid string bad escaping",
			in:   `%\"foo %a"`,
		},
		{
			name:   "valid string with escaped quotes",
			in:     `%"foo %22bar%22 \\ baz"`,
			want:   `%"foo %22bar%22 \\ baz"`,
			wantOk: true,
		},
		{
			name: "invalid sequence id utf-8 string",
			in:   `%"%a0%a1"`,
		},
		{
			name: "invalid 2 bytes sequence utf-8 string",
			in:   `%"%c3%28"`,
		},
		{
			name: "invalid 3 bytes sequence utf-8 string",
			in:   `%"%e2%28%a1"`,
		},
		{
			name: "invalid 4 bytes sequence utf-8 string",
			in:   `%"%f0%28%8c%28"`,
		},
		{
			name: "invalid hex utf-8 string",
			in:   `%"%g0%1w"`,
		},
		{
			name:   "valid byte order mark in display string",
			in:     `%"BOM: %ef%bb%bf"`,
			want:   `%"BOM: %ef%bb%bf"`,
			wantOk: true,
		},
		{
			name:   "valid string with content after",
			in:     `%"foo\nbar", foo;bar`,
			want:   `%"foo\nbar"`,
			wantOk: true,
		},
		{
			name: "invalid unfinished 4 bytes rune",
			in:   `%"%f0%9f%98"`,
		},
	}

	for _, tc := range tests {
		got, gotRest, ok := consumeDisplayString(tc.in)
		if ok != tc.wantOk {
			t.Fatalf("test %q: want ok to be %v, got: %v", tc.name, tc.wantOk, ok)
		}
		if tc.want != got {
			t.Fatalf("test %q: mismatch.\n got: %#v\nwant: %#v\n", tc.name, got, tc.want)
		}
		if got+gotRest != tc.in {
			t.Fatalf("test %q: %#v + %#v != %#v", tc.name, got, gotRest, tc.in)
		}
	}
}

func TestParseDisplayString(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		want   string
		wantOk bool
	}{
		{
			name:   "valid ascii string",
			in:     "%\" !%22#$%25&'()*+,-./0123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_`abcdefghijklmnopqrstuvwxyz{|}~\"",
			want:   " !\"#$%&'()*+,-./0123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_`abcdefghijklmnopqrstuvwxyz{|}~",
			wantOk: true,
		},
		{
			name:   "valid lowercase non-ascii string",
			in:     `%"f%c3%bc%c3%bc"`,
			want:   "füü",
			wantOk: true,
		},
		{
			name: "invalid uppercase non-ascii string",
			in:   `%"f%C3%BC%C3%BC"`,
		},
		{
			name: "invalid unquoted string",
			in:   "%foo",
		},
		{
			name: "invalid string missing initial quote",
			in:   `%foo"`,
		},
		{
			name: "invalid string missing closing quote",
			in:   `%"foo`,
		},
		{
			name: "invalid tab in string",
			in:   "%\"\t\"",
		},
		{
			name: "invalid newline in string",
			in:   "%\"\n\"",
		},
		{
			name: "invalid single quoted string",
			in:   `%'foo'`,
		},
		{
			name: "invalid string bad escaping",
			in:   `%\"foo %a"`,
		},
		{
			name:   "valid string with escaped quotes",
			in:     "%\"foo %22bar%22 \\ baz\"",
			want:   "foo \"bar\" \\ baz",
			wantOk: true,
		},
		{
			name: "invalid sequence id utf-8 string",
			in:   `%"%a0%a1"`,
		},
		{
			name: "invalid 2 bytes sequence utf-8 string",
			in:   `%"%c3%28"`,
		},
		{
			name: "invalid 3 bytes sequence utf-8 string",
			in:   `%"%e2%28%a1"`,
		},
		{
			name: "invalid 4 bytes sequence utf-8 string",
			in:   `%"%f0%28%8c%28"`,
		},
		{
			name: "invalid hex utf-8 string",
			in:   `%"%g0%1w"`,
		},
		{
			name:   "valid byte order mark in display string",
			in:     `%"BOM: %ef%bb%bf"`,
			want:   "BOM: \uFEFF",
			wantOk: true,
		},
		{
			name: "valid string with content after",
			in:   `%"foo\nbar", foo;bar`,
		},
		{
			name: "invalid unfinished 4 bytes rune",
			in:   `%"%f0%9f%98"`,
		},
	}

	for _, tc := range tests {
		got, ok := ParseDisplayString(tc.in)
		if ok != tc.wantOk {
			t.Fatalf("test %q: want ok to be %v, got: %v", tc.name, tc.wantOk, ok)
		}
		if tc.want != got {
			t.Fatalf("test %q: mismatch.\n got: %#v\nwant: %#v\n", tc.name, got, tc.want)
		}
	}
}
