package cli

import (
	"strings"
	"testing"
)

func TestFixCJKEmphasis(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "cjk punctuation bold",
			input: "**测试，**更多",
			want:  "**测试，** 更多",
		},
		{
			name:  "cjk punctuation bold with period",
			input: "**测试。**更多",
			want:  "**测试。** 更多",
		},
		{
			name:  "cjk punctuation bold with exclamation",
			input: "**好！**然后",
			want:  "**好！** 然后",
		},
		{
			name:  "non-punctuation cjk unchanged",
			input: "**中文**词",
			want:  "**中文**词",
		},
		{
			name:  "english unchanged",
			input: "**bold** text",
			want:  "**bold** text",
		},
		{
			name:  "cjk after opening unchanged",
			input: "前**加粗**后",
			want:  "前**加粗**后",
		},
		{
			name:  "inline code untouched",
			input: "`a**中文**b`",
			want:  "`a**中文**b`",
		},
		{
			name:  "fenced code untouched",
			input: "```\n**测试，**更多\n```",
			want:  "```\n**测试，**更多\n```",
		},
		{
			name:  "code span with cjk punctuation",
			input: "`**你好，**世界` and **真，**好",
			want:  "`**你好，**世界` and **真，** 好",
		},
		{
			name:  "multiple emphasis",
			input: "**第一，**和**第二，**都",
			want:  "**第一，** 和**第二，** 都",
		},
		{
			name:  "empty input",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fixCJKEmphasis(tt.input)
			if got != tt.want {
				t.Errorf("fixCJKEmphasis(%q)\n  got:  %q\n  want: %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFixCJKEmphasisRenderIntegration(t *testing.T) {
	r := newMarkdownRenderer(80)

	tests := []struct {
		name     string
		input    string
		wantText string // rendered output must contain this text
	}{
		{
			name:     "cjk punctuation bold renders",
			input:    "**测试，**更多",
			wantText: "测试，",
		},
		{
			name:     "non-punctuation cjk already renders",
			input:    "**中文**词",
			wantText: "中文",
		},
		{
			name:     "inline code preserved",
			input:    "`a**中文**b`",
			wantText: "a**中文**b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rendered := r.Render(tt.input)
			if rendered == "" {
				t.Fatal("Render returned empty string")
			}
			if !strings.Contains(rendered, tt.wantText) {
				t.Errorf("rendered output missing %q:\n%s", tt.wantText, rendered)
			}
		})
	}
}

func TestIsCJKPunct(t *testing.T) {
	tests := []struct {
		r    rune
		want bool
	}{
		{',', false}, // ASCII comma
		{'。', true},  // CJK period
		{'，', true},  // CJK comma
		{'！', true},  // CJK exclamation
		{'？', true},  // CJK question
		{'中', false}, // CJK letter
		{'文', false}, // CJK letter
		{'a', false}, // ASCII letter
		{'「', true},  // CJK bracket
		{'」', true},  // CJK bracket
		{'、', true},  // CJK ideographic comma
		{'·', true},  // middle dot
	}
	for _, tt := range tests {
		if got := isCJKPunct(tt.r); got != tt.want {
			t.Errorf("isCJKPunct(%q) = %v, want %v", tt.r, got, tt.want)
		}
	}
}
