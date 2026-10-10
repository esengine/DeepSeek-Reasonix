package browser

import (
	"fmt"
	"hash/fnv"
	"strings"
	"testing"
)

// fuzzTokens, the generator and the digest are mirrored in
// desktop/electron/test/unit.mjs: both sides must reach fuzzDigest.
var fuzzTokens = []string{
	"file:", "file:/", "/", "//", "\\", ":", "%", "5C", "2F", "09", "0A", ".", "..", "#", "?",
	"D", "c", "$", " ", "h", "x", "\t", "\n", "\r", "\ufeff", "\u0085", "\u0001", "\u007f",
	"localhost", "C:", "UNC", "%5c", "%2e", "?\\", " ",
}

const (
	fuzzCases  = 30000
	fuzzDigest = "67b363c1"
)

func fuzzInputs() []string {
	a := uint32(12321)
	next := func() uint32 {
		a += 0x6D2B79F5
		t := a
		t = (t ^ (t >> 15)) * (t | 1)
		t ^= t + (t^(t>>7))*(t|61)
		return t ^ (t >> 14)
	}
	out := make([]string, 0, fuzzCases)
	for range fuzzCases {
		var b strings.Builder
		if next()%2 == 0 {
			b.WriteString("file:")
		}
		for n := 1 + int(next()%12); n > 0; n-- {
			b.WriteString(fuzzTokens[int(next())%len(fuzzTokens)])
		}
		out = append(out, b.String())
	}
	return out
}

func TestLocalPathURLSeededFuzz(t *testing.T) {
	h := fnv.New32a()
	accepted := 0
	for _, in := range fuzzInputs() {
		got, kind := LocalPathURL(in)
		fmt.Fprintf(h, "%s\x00%s\x00%s\n", in, got, kind)
		if kind != LocalPath {
			continue
		}
		accepted++
		if !strings.HasPrefix(got, "file:///") || strings.HasPrefix(got, "file:////") {
			t.Fatalf("%q was accepted as %q, which is not a plain local file URL", in, got)
		}
		for _, r := range got {
			if r < 0x20 || r == 0x7f {
				t.Fatalf("%q was accepted as %q with a control character", in, got)
			}
		}
	}
	if accepted < 1000 {
		t.Fatalf("only %d accepted: the generator no longer reaches the accept path", accepted)
	}
	if got := fmt.Sprintf("%08x", h.Sum32()); got != fuzzDigest {
		t.Errorf("digest %s, want %s: the Go and JS parsers have moved apart or the corpus changed", got, fuzzDigest)
	}
}
