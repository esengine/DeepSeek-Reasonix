package builtin

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Mutation receipts are appended to the provider-visible conversation after
// every edit, so keep their dynamic tail bounded. The receipt contains only the
// matched and replacement spans, never unchanged same-line or neighboring data.
const maxPostWriteReceiptBytes = 2048
const maxCapturedReceiptSpanBytes = 896

const (
	postWriteSpanTruncated    = "…[replacement span truncated]…"
	postWriteReceiptTruncated = "…[replacement receipt truncated; use read_file for complete current contents]…"
)

func withActualPostWriteReceipts(summary string, receipts []editReplacementReceipt) string {
	if len(receipts) == 0 {
		return summary
	}
	body := renderPostWriteReceipts(receipts)
	if strings.TrimSpace(body) == "" {
		return summary
	}
	return summary + "\nActual replacement receipt after write:\n" + body
}

func renderPostWriteReceipts(receipts []editReplacementReceipt) string {
	indexes := receiptIndexes(len(receipts))
	if len(indexes) == 0 {
		return ""
	}

	// Share the bounded body between the selected first/last receipts and their
	// matched/replacement fields. The final clip below remains a defensive cap
	// for unusually large counts or labels.
	fieldBudget := (maxPostWriteReceiptBytes - 256) / (2 * len(indexes))
	if fieldBudget < 128 {
		fieldBudget = 128
	}
	if fieldBudget > maxCapturedReceiptSpanBytes {
		fieldBudget = maxCapturedReceiptSpanBytes
	}

	var b strings.Builder
	b.Grow(maxPostWriteReceiptBytes)
	for pos, idx := range indexes {
		if pos > 0 {
			b.WriteByte('\n')
		}
		if len(receipts) > 2 && pos == 1 {
			fmt.Fprintf(&b, "…[%d intermediate replacement receipt(s) omitted]…\n\n", len(receipts)-2)
		}
		r := receipts[idx]
		occurrences := r.occurrences
		if occurrences <= 0 {
			occurrences = 1
		}
		fuzzy := ""
		if r.fuzzy {
			fuzzy = ", fuzzy match"
			if occurrences > 1 {
				fuzzy += ", first matched sample shown"
			}
		}
		fmt.Fprintf(&b, "@@ replacement %d of %d (%d occurrence(s)%s) @@\n", idx+1, len(receipts), occurrences, fuzzy)
		appendReceiptSpan(&b, '-', clipPostWriteSpan(r.matched, fieldBudget))
		appendReceiptSpan(&b, '+', clipPostWriteSpan(r.replacement, fieldBudget))
	}
	return clipPostWriteReceipt(b.String())
}

func receiptIndexes(count int) []int {
	switch count {
	case 0:
		return nil
	case 1:
		return []int{0}
	case 2:
		return []int{0, 1}
	default:
		return []int{0, count - 1}
	}
}

func appendReceiptSpan(b *strings.Builder, prefix byte, text string) {
	if text == "" {
		b.WriteByte(prefix)
		b.WriteString("<empty>\n")
		return
	}
	for len(text) > 0 {
		line := text
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			line = text[:i]
			text = text[i+1:]
		} else {
			text = ""
		}
		b.WriteByte(prefix)
		b.WriteString(line)
		b.WriteByte('\n')
	}
}

func clipPostWriteSpan(text string, budget int) string {
	if len(text) <= budget {
		return text
	}
	marker := "\n" + postWriteSpanTruncated + "\n"
	return clipUTF8HeadTail(text, budget, marker)
}

func clipPostWriteReceipt(text string) string {
	if len(text) <= maxPostWriteReceiptBytes {
		return text
	}
	marker := "\n" + postWriteReceiptTruncated + "\n"
	return clipUTF8HeadTail(text, maxPostWriteReceiptBytes, marker)
}

func clipUTF8HeadTail(text string, budget int, marker string) string {
	available := budget - len(marker)
	if available <= 0 {
		return clipUTF8Prefix(marker, budget)
	}
	headBytes := available * 3 / 4
	tailBytes := available - headBytes
	headEnd := utf8PrefixBoundary(text, headBytes)
	tailStart := utf8SuffixBoundary(text, len(text)-tailBytes)
	return text[:headEnd] + marker + text[tailStart:]
}

func clipUTF8Prefix(text string, end int) string {
	if end >= len(text) {
		return text
	}
	return text[:utf8PrefixBoundary(text, end)]
}

func utf8PrefixBoundary(text string, end int) int {
	if end >= len(text) {
		return len(text)
	}
	if end < 0 {
		return 0
	}
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}
	return end
}

func utf8SuffixBoundary(text string, start int) int {
	if start <= 0 {
		return 0
	}
	if start >= len(text) {
		return len(text)
	}
	for start < len(text) && !utf8.RuneStart(text[start]) {
		start++
	}
	return start
}
