package builtin

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestClipPostWriteSpanPreservesUTF8AndBothEnds(t *testing.T) {
	text := "head\n" + strings.Repeat("修改后的内容\n", 800) + "tail\n"
	got := clipPostWriteSpan(text, 512)

	if len(got) > 512 {
		t.Fatalf("clipped span = %d bytes, want at most 512", len(got))
	}
	if !utf8.ValidString(got) {
		t.Fatal("clipped span split a UTF-8 rune")
	}
	for _, want := range []string{"head\n", "tail\n", postWriteSpanTruncated} {
		if !strings.Contains(got, want) {
			t.Fatalf("clipped span should preserve %q:\n%s", want, got)
		}
	}
}

func TestRenderPostWriteReceiptsKeepsFirstAndLastBounded(t *testing.T) {
	receipts := []editReplacementReceipt{
		{matched: "first-old", replacement: "first-new", occurrences: 1},
		{matched: "middle-old", replacement: "middle-new", occurrences: 2},
		{matched: strings.Repeat("最后一项", 500), replacement: "last-new", occurrences: 1, fuzzy: true},
	}
	got := renderPostWriteReceipts(receipts)

	if len(got) > maxPostWriteReceiptBytes {
		t.Fatalf("receipt = %d bytes, want at most %d", len(got), maxPostWriteReceiptBytes)
	}
	if !utf8.ValidString(got) {
		t.Fatal("receipt split a UTF-8 rune")
	}
	for _, want := range []string{"-first-old", "+first-new", "+last-new", "1 intermediate replacement receipt(s) omitted", "fuzzy match", postWriteSpanTruncated} {
		if !strings.Contains(got, want) {
			t.Fatalf("receipt should contain %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "middle-old") || strings.Contains(got, "middle-new") {
		t.Fatalf("bounded receipt should omit intermediate spans:\n%s", got)
	}
}

func TestPostWriteReceiptLargeSpanAllocationIsBounded(t *testing.T) {
	large := strings.Repeat("large replacement payload\n", 700_000)
	receipts := []editReplacementReceipt{{matched: large, replacement: large, occurrences: 1}}
	var got string
	result := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			got = withActualPostWriteReceipts("edited large.txt", receipts)
		}
	})
	if len(got) > maxPostWriteReceiptBytes+256 {
		t.Fatalf("bounded result is too large: %d bytes", len(got))
	}
	if bytes := result.AllocedBytesPerOp(); bytes > 128<<10 {
		t.Fatalf("receipt rendering allocated %d bytes/op for a large span, want at most 128 KiB", bytes)
	}
}

func TestMatchedRangeSampleIsCapturedBounded(t *testing.T) {
	content := strings.Repeat("actual fuzzy match content\n", 2_000)
	got := matchedRangeSample(content, "fallback", []editRange{{start: 0, end: len(content)}})
	if len(got) > maxCapturedReceiptSpanBytes {
		t.Fatalf("captured fuzzy sample = %d bytes, want at most %d", len(got), maxCapturedReceiptSpanBytes)
	}
	if !strings.Contains(got, postWriteSpanTruncated) {
		t.Fatalf("captured fuzzy sample should disclose truncation:\n%s", got)
	}
}
