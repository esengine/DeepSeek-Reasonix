package cli

import (
	"strings"
	"testing"
)

func TestLatexToUnicode(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`E = mc^2`, "E = mc²"},
		{`x^{n+1}`, "xⁿ⁺¹"},
		{`H_2O`, "H₂O"},
		{`x_i^2`, "xᵢ²"},
		{`\alpha + \beta = \gamma`, "α + β = γ"},
		{`\sum_{i=1}^{n} i`, "∑ᵢ₌₁ⁿ i"},
		{`\int_0^1 x\,dx`, "∫₀¹ x dx"},
		{`\frac{1}{2}`, "1/2"},
		{`\frac{x+1}{2}`, "(x+1)/2"},
		{`\sqrt{2}`, "√2"},
		{`\sqrt{x+y}`, "√(x+y)"},
		{`\sqrt[3]{x}`, "∛x"},
		{`a \leq b \neq c`, "a ≤ b ≠ c"},
		{`\mathbb{R}^n`, "ℝⁿ"},
		{`\text{if } x > 0`, "if  x > 0"},
		{`\vec{v}`, "v⃗"},
		{`a^q`, "a^q"},
		{`f(x) = x^{2y}`, "f(x) = x²ʸ"},
	}
	for _, c := range cases {
		if got := latexToUnicode(c.in); got != c.want {
			t.Errorf("latexToUnicode(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeMath(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`\(x+1\)`, "$x+1$"},
		{`\[x+1\]`, "$$x+1$$"},
		{"$$\nE = mc^2\n$$", "$$ E = mc^2 $$"},
		{"`\\(literal\\)`", "`\\(literal\\)`"},
		{"```\n\\[code\\]\n```", "```\n\\[code\\]\n```"},
	}
	for _, c := range cases {
		if got := normalizeMath(c.in); got != c.want {
			t.Errorf("normalizeMath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRenderInlineMath(t *testing.T) {
	colorEnabled = false
	r := newMarkdownRenderer(80)

	out := r.Render(`The mass-energy relation is $E = mc^2$ exactly.`)
	if !strings.Contains(out, "E = mc²") {
		t.Errorf("inline math not rendered: %q", out)
	}

	out = r.Render(`It costs $5 and then $10 total.`)
	if !strings.Contains(out, "$5 and then $10") {
		t.Errorf("currency wrongly parsed as math: %q", out)
	}

	out = r.Render("$$\n\\int_0^1 x\\,dx = \\frac{1}{2}\n$$")
	if !strings.Contains(out, "∫₀¹ x dx = 1/2") {
		t.Errorf("display math not rendered: %q", out)
	}

	out = r.Render("Code stays literal: `$x^2$` here.")
	if !strings.Contains(out, "$x^2$") {
		t.Errorf("math inside code span was converted: %q", out)
	}
}
