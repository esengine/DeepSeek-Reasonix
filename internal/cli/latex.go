package cli

import (
	"strings"
	"unicode/utf8"
)

// latexToUnicode renders a LaTeX math expression as a best-effort Unicode
// approximation suitable for a terminal: Greek letters, operators/relations,
// super/subscripts, \frac, \sqrt and accents map to real glyphs; anything it
// can't represent degrades to a readable plain-text form. There is no Go
// library for this, so the symbol table below is maintained by hand.
func latexToUnicode(expr string) string {
	return convertMath([]rune(expr))
}

func convertMath(rs []rune) string {
	var b strings.Builder
	b.Grow(len(rs))
	for i := 0; i < len(rs); {
		switch r := rs[i]; r {
		case '\\':
			i = convertCommand(&b, rs, i)
		case '^':
			arg, ni := readAtom(rs, i+1, false)
			b.WriteString(superscript(convertMath([]rune(arg))))
			i = ni
		case '_':
			arg, ni := readAtom(rs, i+1, false)
			b.WriteString(subscript(convertMath([]rune(arg))))
			i = ni
		case '{', '}':
			i++
		case '&', '~':
			b.WriteByte(' ')
			i++
		case '$':
			i++
		default:
			b.WriteRune(r)
			i++
		}
	}
	return b.String()
}

// convertCommand consumes the backslash command starting at rs[i] and writes
// its rendering; it returns the index just past everything it consumed.
func convertCommand(b *strings.Builder, rs []rune, i int) int {
	j := i + 1
	if j >= len(rs) {
		return j
	}
	if !isASCIILetter(rs[j]) {
		switch ch := rs[j]; ch {
		case '\\':
			b.WriteString("  ")
		case ',', ';', ':', '!', ' ':
			b.WriteByte(' ')
		default:
			b.WriteRune(ch)
		}
		return j + 1
	}

	k := j
	for k < len(rs) && isASCIILetter(rs[k]) {
		k++
	}
	cmd := string(rs[j:k])

	switch cmd {
	case "frac", "tfrac", "dfrac":
		num, k2 := readAtom(rs, k, true)
		den, k3 := readAtom(rs, k2, true)
		b.WriteString(renderFrac(convertMath([]rune(num)), convertMath([]rune(den))))
		return k3
	case "sqrt":
		idx := ""
		if k < len(rs) && rs[k] == '[' {
			idx, k = readBracket(rs, k)
		}
		arg, k2 := readAtom(rs, k, true)
		b.WriteString(renderSqrt(idx, convertMath([]rune(arg))))
		return k2
	case "text", "textrm", "textbf", "textit", "mathrm", "mathsf", "mathtt", "mathit", "mathbf", "mathcal", "operatorname":
		arg, k2 := readAtom(rs, k, true)
		b.WriteString(arg)
		return k2
	case "mathbb":
		arg, k2 := readAtom(rs, k, true)
		b.WriteString(blackboard(arg))
		return k2
	case "left", "right", "big", "Big", "bigg", "Bigg", "bigl", "bigr", "Bigl", "Bigr", "displaystyle", "textstyle", "limits", "nolimits":
		return k
	case "begin", "end":
		_, k2 := readAtom(rs, k, true)
		return k2
	}

	if combining, ok := accents[cmd]; ok {
		arg, k2 := readAtom(rs, k, true)
		b.WriteString(applyCombining(convertMath([]rune(arg)), combining))
		return k2
	}
	if sym, ok := symbols[cmd]; ok {
		b.WriteString(sym)
		return k
	}
	b.WriteString(cmd)
	return k
}

// readAtom reads the argument of a command or script: a {balanced group}, a
// \command, or a single rune. Returns the inner text (no surrounding braces)
// and the index just past it.
func readAtom(rs []rune, i int, skipSpaces bool) (string, int) {
	if skipSpaces {
		for i < len(rs) && rs[i] == ' ' {
			i++
		}
	}
	if i >= len(rs) {
		return "", i
	}
	switch rs[i] {
	case '{':
		depth := 0
		start := i + 1
		for j := i; j < len(rs); j++ {
			switch rs[j] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					return string(rs[start:j]), j + 1
				}
			}
		}
		return string(rs[start:]), len(rs)
	case '\\':
		k := i + 1
		if k < len(rs) && !isASCIILetter(rs[k]) {
			return string(rs[i : k+1]), k + 1
		}
		for k < len(rs) && isASCIILetter(rs[k]) {
			k++
		}
		return string(rs[i:k]), k
	default:
		return string(rs[i]), i + 1
	}
}

func readBracket(rs []rune, i int) (string, int) {
	start := i + 1
	for j := start; j < len(rs); j++ {
		if rs[j] == ']' {
			return string(rs[start:j]), j + 1
		}
	}
	return "", len(rs)
}

func renderFrac(num, den string) string {
	return wrapIfCompound(num) + "/" + wrapIfCompound(den)
}

func renderSqrt(idx, arg string) string {
	if utf8.RuneCountInString(arg) > 1 {
		arg = "(" + arg + ")"
	}
	switch idx {
	case "", "2":
		return "√" + arg
	case "3":
		return "∛" + arg
	case "4":
		return "∜" + arg
	}
	return superscript(idx) + "√" + arg
}

func wrapIfCompound(s string) string {
	if utf8.RuneCountInString(s) > 1 {
		return "(" + s + ")"
	}
	return s
}

func applyCombining(s string, mark rune) string {
	rs := []rune(s)
	if len(rs) == 0 {
		return string(mark)
	}
	return string(rs[0]) + string(mark) + string(rs[1:])
}

func blackboard(s string) string {
	var b strings.Builder
	for _, r := range s {
		if bb, ok := blackboardCaps[r]; ok {
			b.WriteRune(bb)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func superscript(s string) string {
	if t, ok := mapAll(s, superMap); ok {
		return t
	}
	if utf8.RuneCountInString(s) == 1 {
		return "^" + s
	}
	return "^(" + s + ")"
}

func subscript(s string) string {
	if t, ok := mapAll(s, subMap); ok {
		return t
	}
	if utf8.RuneCountInString(s) == 1 {
		return "_" + s
	}
	return "_(" + s + ")"
}

func mapAll(s string, m map[rune]rune) (string, bool) {
	if s == "" {
		return "", true
	}
	var b strings.Builder
	for _, r := range s {
		c, ok := m[r]
		if !ok {
			return "", false
		}
		b.WriteRune(c)
	}
	return b.String(), true
}

func isASCIILetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// normalizeMath rewrites the alternate math delimiters \(..\) and \[..\] to
// $..$ / $$..$$ and collapses newlines inside a $$ display block onto one line
// so the inline math parser sees a single contiguous run. It tracks fenced and
// inline code so literal delimiters inside code are never rewritten.
func normalizeMath(s string) string {
	rs := []rune(s)
	n := len(rs)
	var b strings.Builder
	b.Grow(len(s))

	inFenced, inCode, inDisplay := false, false, false

	for i := 0; i < n; {
		r := rs[i]

		if r == '`' && i+2 < n && rs[i+1] == '`' && rs[i+2] == '`' {
			inFenced = !inFenced
			b.WriteString("```")
			i += 3
			continue
		}
		if r == '`' && !inFenced {
			inCode = !inCode
			b.WriteRune(r)
			i++
			continue
		}
		if inFenced || inCode {
			b.WriteRune(r)
			i++
			continue
		}

		if r == '\\' && i+1 < n {
			switch rs[i+1] {
			case '\\':
				b.WriteString("\\\\")
				i += 2
				continue
			case '[':
				b.WriteString("$$")
				inDisplay = true
				i += 2
				continue
			case ']':
				b.WriteString("$$")
				inDisplay = false
				i += 2
				continue
			case '(':
				b.WriteString("$")
				i += 2
				continue
			case ')':
				b.WriteString("$")
				i += 2
				continue
			}
		}
		if r == '$' && i+1 < n && rs[i+1] == '$' {
			b.WriteString("$$")
			inDisplay = !inDisplay
			i += 2
			continue
		}
		if r == '\n' && inDisplay {
			b.WriteByte(' ')
			i++
			continue
		}

		b.WriteRune(r)
		i++
	}
	return b.String()
}

var symbols = map[string]string{
	"alpha": "α", "beta": "β", "gamma": "γ", "delta": "δ", "epsilon": "ε",
	"varepsilon": "ε", "zeta": "ζ", "eta": "η", "theta": "θ", "vartheta": "ϑ",
	"iota": "ι", "kappa": "κ", "lambda": "λ", "mu": "μ", "nu": "ν", "xi": "ξ",
	"omicron": "ο", "pi": "π", "varpi": "ϖ", "rho": "ρ", "varrho": "ϱ",
	"sigma": "σ", "varsigma": "ς", "tau": "τ", "upsilon": "υ", "phi": "φ",
	"varphi": "ϕ", "chi": "χ", "psi": "ψ", "omega": "ω",
	"Gamma": "Γ", "Delta": "Δ", "Theta": "Θ", "Lambda": "Λ", "Xi": "Ξ",
	"Pi": "Π", "Sigma": "Σ", "Upsilon": "Υ", "Phi": "Φ", "Psi": "Ψ", "Omega": "Ω",

	"times": "×", "div": "÷", "cdot": "·", "ast": "∗", "star": "⋆",
	"pm": "±", "mp": "∓", "oplus": "⊕", "ominus": "⊖", "otimes": "⊗",
	"oslash": "⊘", "odot": "⊙", "circ": "∘", "bullet": "•", "setminus": "∖",

	"leq": "≤", "le": "≤", "geq": "≥", "ge": "≥", "neq": "≠", "ne": "≠",
	"equiv": "≡", "approx": "≈", "cong": "≅", "sim": "∼", "simeq": "≃",
	"propto": "∝", "ll": "≪", "gg": "≫", "doteq": "≐", "asymp": "≍",

	"leftarrow": "←", "rightarrow": "→", "to": "→", "gets": "←",
	"leftrightarrow": "↔", "Leftarrow": "⇐", "Rightarrow": "⇒",
	"Leftrightarrow": "⇔", "implies": "⇒", "iff": "⇔", "mapsto": "↦",
	"uparrow": "↑", "downarrow": "↓", "longrightarrow": "⟶", "longleftarrow": "⟵",

	"sum": "∑", "prod": "∏", "coprod": "∐", "int": "∫", "iint": "∬",
	"iiint": "∭", "oint": "∮", "nabla": "∇", "partial": "∂",
	"infty": "∞", "sqrt": "√", "surd": "√",

	"in": "∈", "notin": "∉", "ni": "∋", "subset": "⊂", "supset": "⊃",
	"subseteq": "⊆", "supseteq": "⊇", "cup": "∪", "cap": "∩",
	"emptyset": "∅", "varnothing": "∅", "forall": "∀", "exists": "∃",
	"nexists": "∄", "neg": "¬", "lnot": "¬", "land": "∧", "wedge": "∧",
	"lor": "∨", "vee": "∨",

	"angle": "∠", "perp": "⊥", "parallel": "∥", "mid": "∣", "nmid": "∤",
	"triangle": "△", "square": "□", "diamond": "◇", "top": "⊤", "bot": "⊥",
	"vdash": "⊢", "models": "⊨", "therefore": "∴", "because": "∵",

	"ldots": "…", "dots": "…", "cdots": "⋯", "vdots": "⋮", "ddots": "⋱",
	"prime": "′", "degree": "°", "deg": "°", "hbar": "ℏ", "ell": "ℓ",
	"Re": "ℜ", "Im": "ℑ", "aleph": "ℵ", "wp": "℘",
	"langle": "⟨", "rangle": "⟩", "lceil": "⌈", "rceil": "⌉",
	"lfloor": "⌊", "rfloor": "⌋", "backslash": "\\",

	"quad": "  ", "qquad": "    ", "space": " ", "thinspace": " ",
	"lim": "lim", "sin": "sin", "cos": "cos", "tan": "tan", "log": "log",
	"ln": "ln", "exp": "exp", "min": "min", "max": "max", "det": "det",
	"gcd": "gcd", "dim": "dim", "ker": "ker",
}

var accents = map[string]rune{
	"hat": '̂', "widehat": '̂', "bar": '̄', "overline": '̄',
	"vec": '⃗', "dot": '̇', "ddot": '̈', "tilde": '̃',
	"widetilde": '̃', "acute": '́', "grave": '̀', "check": '̌',
}

var superMap = map[rune]rune{
	'0': '⁰', '1': '¹', '2': '²', '3': '³', '4': '⁴', '5': '⁵', '6': '⁶',
	'7': '⁷', '8': '⁸', '9': '⁹', '+': '⁺', '-': '⁻', '=': '⁼', '(': '⁽',
	')': '⁾', 'a': 'ᵃ', 'b': 'ᵇ', 'c': 'ᶜ', 'd': 'ᵈ', 'e': 'ᵉ', 'f': 'ᶠ',
	'g': 'ᵍ', 'h': 'ʰ', 'i': 'ⁱ', 'j': 'ʲ', 'k': 'ᵏ', 'l': 'ˡ', 'm': 'ᵐ',
	'n': 'ⁿ', 'o': 'ᵒ', 'p': 'ᵖ', 'r': 'ʳ', 's': 'ˢ', 't': 'ᵗ', 'u': 'ᵘ',
	'v': 'ᵛ', 'w': 'ʷ', 'x': 'ˣ', 'y': 'ʸ', 'z': 'ᶻ',
}

var subMap = map[rune]rune{
	'0': '₀', '1': '₁', '2': '₂', '3': '₃', '4': '₄', '5': '₅', '6': '₆',
	'7': '₇', '8': '₈', '9': '₉', '+': '₊', '-': '₋', '=': '₌', '(': '₍',
	')': '₎', 'a': 'ₐ', 'e': 'ₑ', 'h': 'ₕ', 'i': 'ᵢ', 'j': 'ⱼ', 'k': 'ₖ',
	'l': 'ₗ', 'm': 'ₘ', 'n': 'ₙ', 'o': 'ₒ', 'p': 'ₚ', 'r': 'ᵣ', 's': 'ₛ',
	't': 'ₜ', 'u': 'ᵤ', 'v': 'ᵥ', 'x': 'ₓ',
}

var blackboardCaps = map[rune]rune{
	'A': '𝔸', 'B': '𝔹', 'C': 'ℂ', 'D': '𝔻', 'E': '𝔼', 'F': '𝔽', 'G': '𝔾',
	'H': 'ℍ', 'I': '𝕀', 'J': '𝕁', 'K': '𝕂', 'L': '𝕃', 'M': '𝕄', 'N': 'ℕ',
	'O': '𝕆', 'P': 'ℙ', 'Q': 'ℚ', 'R': 'ℝ', 'S': '𝕊', 'T': '𝕋', 'U': '𝕌',
	'V': '𝕍', 'W': '𝕎', 'X': '𝕏', 'Y': '𝕐', 'Z': 'ℤ',
}
