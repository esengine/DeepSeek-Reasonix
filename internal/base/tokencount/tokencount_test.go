package tokencount

import "testing"

func TestTextDoesNotUndercountCJK(t *testing.T) {
	// The reason this package exists: bytes/4 reads a Chinese transcript as a
	// third of its real size, so a planner sizing context off it compacts late.
	const cjk = "把这个仓库跑一遍测试，把失败的那几个定位到具体文件"
	runes := len([]rune(cjk))
	if got := Text(cjk); got != runes {
		t.Fatalf("Text(cjk) = %d, want one token per rune (%d)", got, runes)
	}
	if byBytes := len(cjk) / 4; Text(cjk) <= byBytes {
		t.Fatalf("Text(cjk) = %d, must exceed the bytes/4 reading (%d)", Text(cjk), byBytes)
	}
}

func TestTextRoundsASCIIUpAndKeepsEmptyFree(t *testing.T) {
	if got := Text(""); got != 0 {
		t.Fatalf("Text(\"\") = %d, want 0", got)
	}
	// Five bytes is more than one token's worth; truncating would let a long
	// run of short strings sum to nothing.
	if got := Text("abcde"); got != 2 {
		t.Fatalf("Text(\"abcde\") = %d, want 2", got)
	}
}
