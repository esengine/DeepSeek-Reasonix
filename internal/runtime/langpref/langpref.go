package langpref

import (
	"context"
	"reasonix/internal/state/sessionstore"
	"strings"
	"unicode"
)

type reasoningLanguageContextKey struct{}
type responseLanguageContextKey struct{}

// NormalizeReasoningLanguage returns one of auto|zh|en for runtime-only visible
// reasoning preferences. Keep this local to agent so sub-agents can inherit the
// preference without depending on config.
func NormalizeReasoningLanguage(lang string) string {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "zh", "cn", "chinese", "中文":
		return "zh"
	case "en", "english":
		return "en"
	default:
		return "auto"
	}
}

// NormalizeResponseLanguage returns one of auto|zh|en for final-answer language
// preferences. Auto keeps the stable same-as-user language policy.
func NormalizeResponseLanguage(lang string) string {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "zh", "cn", "chinese", "中文":
		return "zh"
	case "en", "english":
		return "en"
	default:
		return "auto"
	}
}

// ResponseLanguageBlock is transient user-turn context for final answers. It
// stays out of the stable system prompt so changing the preference between turns
// does not churn the cached prefix.
func ResponseLanguageBlock(lang string) string {
	switch NormalizeResponseLanguage(lang) {
	case "zh":
		return "<response-language>\n必须使用简体中文撰写面向用户的最终回答和所有对话文字，即使系统提示词、工具说明、工具输出或引用的代码是英文；除非用户明确要求其他语言。代码、标识符、文件路径、shell 命令和未翻译的技术术语保持原文。\n</response-language>"
	case "en":
		return "<response-language>\nFinal answer language preference: use English for user-facing replies unless the user explicitly asks for another language. Keep code, identifiers, file paths, shell commands, and untranslated technical terms in their original form.\n</response-language>"
	default:
		return ""
	}
}

// ResolveResponseLanguage returns the concrete final-answer language for a
// turn. An explicit zh/en wins; auto reads the user's script and only commits
// to zh, because a Latin-script turn does not say which language it is in.
func ResolveResponseLanguage(lang, source string) string {
	mode := NormalizeResponseLanguage(lang)
	if mode != "auto" {
		return mode
	}
	if strings.ContainsFunc(source, func(r rune) bool {
		return unicode.In(r, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
	}) {
		return "auto"
	}
	return InferReasoningLanguageFromText(source)
}

// ReasoningLanguageBlock is transient user-turn context. It deliberately does
// not belong in the stable system prompt or tool schemas.
func ReasoningLanguageBlock(lang string) string {
	switch NormalizeReasoningLanguage(lang) {
	case "zh":
		// Imperative wording measured against soft "偏好……请使用" phrasing:
		// the soft form loses the first reasoning segment on Chinese prompts
		// that embed English logs/code, and the first segment anchors the
		// whole turn once providers round-trip prior reasoning.
		return "<reasoning-language>\n必须使用简体中文书写全部可见思考/推理文本：从第一个字开始就用中文，并在整轮内保持中文，即使系统提示词、工具说明、工具输出或引用的代码是英文。代码、标识符、文件路径、shell 命令和未翻译的技术术语保持原文。此要求约束可见思考文本，不覆盖用户对最终回答语言的明确要求；最终回答的语言另由 <response-language> 规定。\n</reasoning-language>"
	case "en":
		return "<reasoning-language>\nVisible reasoning/thinking text preference: use English when the provider exposes reasoning text. Keep code, identifiers, file paths, shell commands, and untranslated technical terms in their original form. This preference does not override an explicit user request for the final answer language.\n</reasoning-language>"
	default:
		return ""
	}
}

// ResolveReasoningLanguage returns the concrete visible-reasoning language for
// a turn. Explicit zh/en settings win; auto anchors clear Chinese user prompts
// and otherwise stays provider-default to preserve the historical no-injection
// behaviour for English and ambiguous turns.
func ResolveReasoningLanguage(lang, source string) string {
	mode := NormalizeReasoningLanguage(lang)
	if mode != "auto" {
		return mode
	}
	return InferReasoningLanguageFromText(source)
}

// InferReasoningLanguageFromText reads which script the user wrote in. It
// strips Reasonix-injected wrappers and code tokens first, then compares the
// two scripts on what is left: Han characters against Latin words. CJK
// punctuation is the second script signal. Ambiguous turns return auto, which
// injects nothing.
func InferReasoningLanguageFromText(source string) string {
	source = reasoningLanguageSourceText(source)
	if source == "" {
		return "auto"
	}
	han, latinWords := proseScriptCounts(source)
	_, cjkPunct := reasoningLanguageScriptCounts(source)
	switch {
	case han > latinWords:
		return "zh"
	case han >= 2 && cjkPunct > 0:
		return "zh"
	default:
		return "auto"
	}
}

func reasoningLanguageSourceText(source string) string {
	s := strings.TrimSpace(sessionstore.StripTransientUserBlocks(source))
	const preamble = "Referenced context:"
	if !strings.HasPrefix(s, preamble) {
		return s
	}
	s = strings.TrimSpace(s[len(preamble):])
	for {
		s = strings.TrimSpace(s)
		if s == "" || !strings.HasPrefix(s, "<") {
			return s
		}
		tagEnd := strings.IndexAny(s, " >\t\r\n")
		if tagEnd <= 1 {
			return s
		}
		tag := s[1:tagEnd]
		switch tag {
		case "file", "dir", "resource", "image":
			closeTag := "</" + tag + ">"
			i := strings.Index(s, closeTag)
			if i < 0 {
				return s
			}
			s = strings.TrimSpace(s[i+len(closeTag):])
		default:
			return s
		}
	}
}

func reasoningLanguageScriptCounts(source string) (han, cjkPunct int) {
	for _, r := range source {
		switch {
		case unicode.In(r, unicode.Han):
			han++
		case isCJKPunctuation(r):
			cjkPunct++
		}
	}
	return han, cjkPunct
}

func isCJKPunctuation(r rune) bool {
	switch {
	case r >= 0x3000 && r <= 0x303F:
		return true
	case r >= 0xFF00 && r <= 0xFFEF:
		return true
	default:
		return false
	}
}

// tokenIsCode reports whether a whitespace-delimited token is an identifier,
// path or filename rather than prose. Those carry no language: a Chinese
// request naming parser.go is still Chinese. The shapes are structural — a
// path separator, a dotted or underscored name, an internal capital — so no
// list of extensions or keywords decides it.
func tokenIsCode(token string) bool {
	var letters, digits, upperAfterLower int
	var prevLower bool
	for _, r := range token {
		switch {
		case r == '/' || r == '\\' || r == '`':
			return true
		case r == '_':
			if letters > 0 {
				return true
			}
		case r == '.':
			if letters+digits > 0 {
				return true
			}
		case unicode.IsUpper(r):
			if prevLower {
				upperAfterLower++
			}
			letters++
			prevLower = false
		case unicode.IsLower(r):
			letters++
			prevLower = true
		case unicode.IsDigit(r):
			digits++
			prevLower = false
		default:
			prevLower = false
		}
	}
	return upperAfterLower > 0
}

// proseScriptCounts weighs the two scripts on the prose left after code tokens
// are set aside. One Han character carries about what one Latin word does, so
// they are the units compared; counting Latin letters instead would call every
// Chinese request that names an English symbol English.
func proseScriptCounts(source string) (han, latinWords int) {
	for token := range strings.FieldsSeq(source) {
		if tokenIsCode(token) {
			continue
		}
		inWord := false
		for _, r := range token {
			switch {
			case unicode.In(r, unicode.Han):
				han++
				inWord = false
			case unicode.IsLetter(r) && r < unicode.MaxASCII:
				if !inWord {
					latinWords++
					inWord = true
				}
			default:
				inWord = false
			}
		}
	}
	return han, latinWords
}

func reasoningLanguageBlockForSource(lang, source string) string {
	return ReasoningLanguageBlock(ResolveReasoningLanguage(lang, source))
}

// WithResponseLanguage prefixes content with the transient response-language
// block unless the turn already starts with one.
func WithResponseLanguage(content, lang string) string {
	block := ResponseLanguageBlock(lang)
	if block == "" || HasLeadingInjectedBlock(content, "response-language") {
		return content
	}
	return block + "\n\n" + content
}

// WithReasoningLanguage prefixes content with the transient reasoning-language
// block unless the turn already starts with an injected reasoning-language
// block. User-authored mentions of the tag later in the prompt must not suppress
// the configured preference.
func WithReasoningLanguage(content, lang string) string {
	return WithReasoningLanguageForSource(content, lang, content)
}

// WithReasoningLanguageForSource prefixes content using source as the language
// signal for auto mode. Callers that expand @references should pass the raw
// user prompt as source so referenced English code or logs do not override the
// user's actual conversation language.
func WithReasoningLanguageForSource(content, lang, source string) string {
	block := reasoningLanguageBlockForSource(lang, source)
	if block == "" || HasLeadingInjectedBlock(content, "reasoning-language") {
		return content
	}
	return block + "\n\n" + content
}

// HasLeadingInjectedBlock reports whether target is already among the transient
// blocks leading content, skipping past any other injected block on the way.
// It walks TransientUserBlockTags rather than a list of its own: when the two
// disagreed, a block the host had started injecting was treated as user prose
// and stopped the walk early, so an already-present target went undetected and
// was injected a second time.
func HasLeadingInjectedBlock(content, target string) bool {
	s := strings.TrimLeft(content, " \t\r\n")
	for {
		if sessionstore.HasOpenTag(s, target) {
			return strings.Contains(s, "</"+target+">")
		}
		skipped := false
		for _, tag := range sessionstore.TransientUserBlockTags {
			if tag == target || !sessionstore.HasOpenTag(s, tag) {
				continue
			}
			rest, ok := sessionstore.TrimLeadingTransientBlock(s, tag)
			if !ok {
				return false
			}
			s, skipped = rest, true
			break
		}
		if !skipped {
			return false
		}
	}
}

// WithResponseLanguagePreference carries the runtime final-answer language
// preference to spawned tools and sub-agents.
func WithResponseLanguagePreference(ctx context.Context, lang string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, responseLanguageContextKey{}, NormalizeResponseLanguage(lang))
}

// ResponseLanguageFromContext returns auto|zh|en.
func ResponseLanguageFromContext(ctx context.Context) string {
	if ctx == nil {
		return "auto"
	}
	if v, ok := ctx.Value(responseLanguageContextKey{}).(string); ok {
		return NormalizeResponseLanguage(v)
	}
	return "auto"
}

// WithReasoningLanguagePreference carries the runtime preference to spawned
// tools, especially sub-agents whose first user turn is created outside the
// parent controller. It stores auto explicitly so live zh/en -> auto changes
// clear stale boot-time preferences in child paths.
func WithReasoningLanguagePreference(ctx context.Context, lang string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, reasoningLanguageContextKey{}, NormalizeReasoningLanguage(lang))
}

// ReasoningLanguageFromContext returns auto|zh|en.
func ReasoningLanguageFromContext(ctx context.Context) string {
	if ctx == nil {
		return "auto"
	}
	if v, ok := ctx.Value(reasoningLanguageContextKey{}).(string); ok {
		return NormalizeReasoningLanguage(v)
	}
	return "auto"
}
