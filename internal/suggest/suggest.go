// Package suggest predicts the user's likely next prompt from recent
// conversation history, so a UI can offer it as a Tab-accepted ghost-text
// completion. It uses an independent cheap "Lite" provider/model so the main
// (Pro) working model is never spent on these short, low-value calls.
package suggest

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/provider"
)

// systemPromptTemplate is the instruction given to the suggestion model. It
// deliberately limits the model's role (it must never generate content, only
// predict the user's next prompt), follows the user's own voice (not the
// assistant's), and forbids meta-text. languageReq is appended to constrain the
// output language to match the conversation.
const systemPromptTemplate = `You are a prompt suggestion generator. Your ONLY purpose is to predict the user's next action in a coding agent.

Your job:
1. Read the user's last message and the assistant's final answer.
2. Predict what the USER would naturally type next — not what the assistant should do.

CRITICAL CONSTRAINTS:
- You are NOT a code generator, writer, or task executor.
- You MUST respond with ONLY the suggestion text.
- NEVER generate, implement, code, or produce any content.
- NEVER provide explanations, reasoning, or extra text.
- NEVER use quotes, markdown, or formatting.
- Be specific when you can — name files, functions, or actions.
- Say "done" ONLY if the work is truly complete with no natural follow-ups.

THE TEST: would the user think "I was just about to type that"?

EXAMPLES:
User asked "fix the bug and run tests", bug is fixed -> "run the tests"
After code written -> "try it out"
Assistant offers options -> pick the one the user would choose
Assistant asks to continue -> "yes" or "go ahead"
Task complete, obvious follow-up -> "commit this" or "push it"
After an error or misunderstanding -> respond with nothing

NEVER SUGGEST:
- Evaluative ("looks good", "thanks")
- Questions ("what about...?")
- Assistant-voice ("Let me...", "I'll...", "Here's...")
- New ideas the user did not ask about
- Multiple sentences

Reply with ONLY the suggestion, 3-12 words, no quotes or explanation. If the next step is not obvious, reply with nothing.

Language: %s
`

// Options carries per-call limits. Zero values fall back to sane defaults.
type Options struct {
	MaxTokens int // cap on generated suggestion length; 0 => 60
	// Temperature is the sampling temperature. 0 leaves it unset (provider
	// default); a small positive value yields slightly varied suggestions.
	Temperature *float64
}

// ProviderFactory builds a provider.Provider from a resolved provider entry.
// Callers supply boot.NewProvider (suggest must stay below the boot frontend
// layer and therefore never imports it).
type ProviderFactory func(*config.ProviderEntry) (provider.Provider, error)

// Provider resolves a model reference into a provider.Provider suitable for
// generating a suggestion. An empty modelRef falls back to the config's default
// model. modelRef may be "provider/model", a provider name, or a bare model
// name (see config.Config.ResolveModel).
func Provider(cfg *config.Config, modelRef string, newProvider ProviderFactory) (provider.Provider, error) {
	ref := strings.TrimSpace(modelRef)
	if ref == "" {
		ref = cfg.DefaultModel
	}
	entry, ok := cfg.ResolveModel(ref)
	if !ok {
		return nil, fmt.Errorf("suggest: unknown model %q", ref)
	}
	// A next-prompt prediction needs no deep reasoning: disable thinking/effort
	// so the endpoint returns a first token promptly (the default DeepSeek
	// providers ship with Thinking=enabled, Effort=high).
	entry.Thinking = "disabled"
	if strings.TrimSpace(entry.Effort) == "" || entry.Effort == "auto" {
		entry.Effort = "disabled"
	} else if entry.Effort != "disabled" {
		if norm, err := config.NormalizeEffort(entry, "disabled"); err == nil {
			entry.Effort = norm
		}
	}
	p, err := newProvider(entry)
	if err != nil {
		return nil, fmt.Errorf("suggest: build provider for %q: %w", ref, err)
	}
	return p, nil
}

// NextPrompt asks the suggestion provider to predict the user's next prompt
// from the recent conversation history. It returns the raw predicted text,
// trimmed, or "" when the model produced nothing. On error it returns "" and
// the error so callers can silently degrade (a failed suggestion should never
// block the UI).
func NextPrompt(ctx context.Context, p provider.Provider, history []provider.Message, opts Options) (string, error) {
	if p == nil {
		return "", nil
	}
	maxTokens := opts.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 60
	}

	messages := buildMessages(history)
	req := provider.Request{
		Messages:    messages,
		MaxTokens:   maxTokens,
		Temperature: opts.Temperature,
	}
	ch, err := p.Stream(ctx, req)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			b.WriteString(chunk.Text)
		case provider.ChunkError:
			if chunk.Err != nil {
				return "", chunk.Err
			}
		case provider.ChunkDone:
			text := strings.TrimSpace(b.String())
			if shouldFilterSuggestion(text) {
				return "", nil
			}
			return text, nil
		}
	}
	text := strings.TrimSpace(b.String())
	if shouldFilterSuggestion(text) {
		return "", nil
	}
	return text, nil
}

// shouldFilterSuggestion discards raw model output that is not a usable next
// prompt: meta-text ("silence", "no suggestion", "done"), evaluative filler,
// assistant-voice phrasing, over-long output, or formatting. Returns true when
// the output should be hidden (treat it as "no suggestion").
func shouldFilterSuggestion(s string) bool {
	if s == "" {
		return true
	}
	lower := strings.ToLower(s)
	wordCount := len(strings.Fields(s))

	// Meta-text: the model spells out the "stay silent / done" instruction.
	if lower == "done" ||
		lower == "nothing found" ||
		strings.HasPrefix(lower, "nothing to suggest") ||
		strings.HasPrefix(lower, "no suggestion") ||
		strings.HasPrefix(lower, "no follow-up") ||
		silenceMetaRe.MatchString(lower) {
		return true
	}
	// Meta wrapped in punctuation: (silence — ...), [no suggestion].
	if wrappedMetaRe.MatchString(s) {
		return true
	}
	// Error echo the model might pass through.
	if strings.HasPrefix(lower, "api error:") ||
		strings.HasPrefix(lower, "error:") {
		return true
	}
	// Word-count bounds: single tokens are only allowed for known user commands
	// (affirmatives, actions); more than 12 words is too long. CJK text has no
	// word separators, so it is judged by character length instead.
	if hasCJK(s) {
		if len([]rune(s)) < 2 && !allowedSingleWord(lower) {
			return true
		}
	} else {
		if wordCount > 12 {
			return true
		}
		if wordCount < 2 && !allowedSingleWord(lower) {
			return true
		}
	}
	// Length and sentence guards.
	if len(s) >= 100 {
		return true
	}
	if multipleSentenceRe.MatchString(s) {
		return true
	}
	if formattingRe.MatchString(s) {
		return true
	}
	// Evaluative filler.
	if evaluativeRe.MatchString(lower) {
		return true
	}
	// Assistant-voice: the suggestion must read as the USER typing, not the AI.
	if assistantVoiceRe.MatchString(s) {
		return true
	}
	return false
}

var (
	silenceMetaRe      = regexp.MustCompile(`\bsilence\b|\bstay silent\b|\bno more\b`)
	wrappedMetaRe      = regexp.MustCompile(`^\(.*\)$|^\[.*\]$`)
	multipleSentenceRe = regexp.MustCompile(`[.!?]\s+[A-Z]`)
	formattingRe       = regexp.MustCompile(`[\n*]|\*\*`)
	evaluativeRe       = regexp.MustCompile(`thanks|thank you|looks good|sounds good|that works|that worked|that's all|nice|great|perfect|makes sense|awesome|excellent`)
	assistantVoiceRe   = regexp.MustCompile(`^(let me|i'll|i've|i'm|i can|i would|i think|i notice|here's|here is|here are|that's|this is|this will|you can|you should|you could|sure,|of course|certainly)`)
)

var allowedSingleWords = map[string]bool{
	// Affirmatives
	"yes": true, "yeah": true, "yep": true, "yea": true, "yup": true,
	"sure": true, "ok": true, "okay": true,
	// Actions
	"push": true, "commit": true, "deploy": true, "stop": true,
	"continue": true, "check": true, "exit": true, "quit": true, "done": true,
	// Negation
	"no": true,
	// Chinese equivalents
	"继续": true, "好": true, "好的": true, "行": true, "可以": true,
	"提交": true, "推送": true, "停止": true, "退出": true, "完成": true,
	"检查": true, "部署": true, "测试": true, "运行": true,
}

func allowedSingleWord(lower string) bool {
	return allowedSingleWords[lower] || strings.HasPrefix(lower, "/")
}

// buildMessages folds the system instruction and the LAST completed turn into a
// single request. The user's final prompt and the assistant's final answer are
// wrapped in labelled blocks so the model treats them as already-happened
// context (not instructions to act on). Intermediate tool/reasoning steps are
// deliberately omitted — a next-prompt prediction only needs the final exchange.
func buildMessages(history []provider.Message) []provider.Message {
	user, assistant := lastTurn(history)
	language := suggestionLanguage(user)

	out := make([]provider.Message, 0, 3)
	out = append(out, provider.Message{
		Role:    provider.RoleSystem,
		Content: fmt.Sprintf(systemPromptTemplate, language),
	})

	var b strings.Builder
	b.WriteString("[User Message]\n")
	b.WriteString(user)
	b.WriteString("\n\n[Assistant Response]\n")
	b.WriteString(assistant)
	out = append(out, provider.Message{Role: provider.RoleUser, Content: b.String()})
	return out
}

// lastTurn returns the most recent user prompt and the assistant's final answer
// from history. It walks from the end to find the last user message and the last
// final assistant message (an assistant message with no pending tool calls).
// Returns empty strings when the corresponding message is missing.
func lastTurn(history []provider.Message) (user, assistant string) {
	for i := len(history) - 1; i >= 0; i-- {
		m := history[i]
		if m.Role != provider.RoleUser {
			continue
		}
		u := m.Content
		if u == "" {
			u = m.RawContent
		}
		user = strings.TrimSpace(u)
		break
	}
	for i := len(history) - 1; i >= 0; i-- {
		m := history[i]
		// Skip tool-call turns and reasoning-only fragments: only the final
		// text answer is the visible assistant response the user replies to.
		if m.Role != provider.RoleAssistant || len(m.ToolCalls) > 0 {
			continue
		}
		a := strings.TrimSpace(m.Content)
		if a != "" {
			assistant = a
			break
		}
	}
	return user, assistant
}

// suggestionLanguage picks the output language for the suggestion to match the
// user's last prompt. Chinese text (CJK) → 简体中文; otherwise English.
func suggestionLanguage(user string) string {
	if hasCJK(user) {
		return "简体中文"
	}
	return "English"
}

// hasCJK reports whether s contains any CJK ideograph (Chinese/Japanese/Korean
// unified ideographs), which are not space-separated like English words.
func hasCJK(s string) bool {
	for _, r := range s {
		if r >= '\u4e00' && r <= '\u9fff' {
			return true
		}
	}
	return false
}
