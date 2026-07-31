package jsonschema

import (
	"hash/maphash"
	"testing"
)

func TestQuote(t *testing.T) {
	tests := []struct{ input, want string }{
		{`abc"def'ghi`, `'abc"def\'ghi'`},
	}
	for _, test := range tests {
		got := quote(test.input)
		if got != test.want {
			t.Errorf("quote(%q): got %q, want %q", test.input, got, test.want)
		}
	}
}

func TestFragment(t *testing.T) {
	tests := []struct {
		input string
		want  any
	}{
		{"#", jsonPointer("")},
		{"#/a/b", jsonPointer("/a/b")},
		{"#abcd", anchor("abcd")},
		{"#%61%62%63%64", anchor("abcd")},
		{"#%2F%61%62%63%64%2fef", jsonPointer("/abcd/ef")}, // '/' is enchoded
		{"#abcd+ef", anchor("abcd+ef")},                    // '+' should not traslate to space
	}
	for _, test := range tests {
		_, frag, err := splitFragment(test.input)
		if err != nil {
			t.Errorf("splitFragment(%q): %v", test.input, err)
			continue
		}
		got := frag.convert()
		if got != test.want {
			t.Errorf("splitFragment(%q): got %q, want %q", test.input, got, test.want)
		}
	}
}

func TestUnescape(t *testing.T) {
	tests := []struct {
		input, want string
		ok          bool
	}{
		{"bar~0", "bar~", true},
		{"bar~1", "bar/", true},
		{"bar~01", "bar~1", true},
		{"bar~", "", false},
		{"bar~~", "", false},
	}
	for _, test := range tests {
		got, ok := unescape(test.input)
		if ok != test.ok {
			t.Errorf("unescape(%q).ok: got %v, want %v", test.input, ok, test.ok)
			continue
		}
		if got != test.want {
			t.Errorf("unescape(%q): got %q, want %q", test.input, got, test.want)
		}
	}
}

func TestEquals(t *testing.T) {
	tests := []struct {
		v1, v2 any
		want   bool
	}{
		{1.0, 1, true},
		{-1.0, -1, true},
	}
	for _, test := range tests {
		got, k := equals(test.v1, test.v2)
		if k != nil {
			t.Error("got error:", k)
			continue
		}
		if got != test.want {
			t.Errorf("equals(%v, %v): got %v, want %v", test.v1, test.v2, got, test.want)
		}
	}
}

func TestHashEquals(t *testing.T) {
	tests := []struct {
		v1, v2 any
		want   bool
	}{
		{1.0, 1, true},
		{-1.0, -1, true},
	}
	hash := new(maphash.Hash)
	for _, test := range tests {
		hash.Reset()
		writeHash(test.v1, hash)
		h1 := hash.Sum64()
		hash.Reset()
		writeHash(test.v2, hash)
		h2 := hash.Sum64()
		got := h1 == h2
		if got != test.want {
			t.Errorf("hashEquals(%v, %v): got %v, want %v", test.v1, test.v2, got, test.want)
		}
	}
}
