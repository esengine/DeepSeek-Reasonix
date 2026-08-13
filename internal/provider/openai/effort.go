// Effort vocabulary and per-request depth resolution: which reasoning levels
// an endpoint accepts, and how a Request.EffortOverride maps onto them.
package openai

import (
	"strings"

	"reasonix/internal/provider"
)

// configuredThinkingType reads the optional explicit `thinking` config field —
// a vendor-agnostic escape hatch (credit @eghrhegpe, #5063) for OpenAI-
// compatible providers we don't auto-detect (e.g. opencode.ai). Unknown values
// are ignored so a typo never breaks a request.
func configuredThinkingType(cfg provider.Config) string {
	t, _ := cfg.Extra["thinking"].(string)
	t = strings.ToLower(strings.TrimSpace(t))
	if t != "enabled" && t != "disabled" {
		return ""
	}
	return t
}

// effortEndpoint carries the protocol facts New resolved that decide which
// depth levels a per-request override may take.
type effortEndpoint struct {
	protocol     string
	thinkingType string
	effort       string
	deepseek     bool
	flash        bool // deepseek-v4-flash — the only official DeepSeek model with effort=low
	minimax      bool
	zhipu        bool
	longcat      bool
	ollamaCloud  bool
	explicit     bool
	supported    []string
}

// requestEffortVocabulary lists exactly the depth levels New would accept as a
// configured effort for this endpoint. Binary thinking knobs and disabled
// thinking yield nil — an override adjusts depth, never whether thinking runs.
func requestEffortVocabulary(e effortEndpoint) []string {
	if e.thinkingType == "disabled" || e.protocol == "none" || e.minimax || e.zhipu || e.longcat {
		return nil
	}
	var levels []string
	switch {
	case e.explicit:
		levels = e.supported
	case e.deepseek:
		levels = []string{"high", "max"}
		if e.flash {
			levels = append(levels, "low")
		}
	case e.ollamaCloud:
		levels = []string{"low", "medium", "high", "max"}
	case e.effort != "":
		levels = []string{"low", "medium", "high"}
	}
	return depthOnly(levels)
}

// depthOnly strips thinking on/off toggles from an effort vocabulary.
func depthOnly(levels []string) []string {
	var out []string
	for _, level := range levels {
		switch strings.ToLower(strings.TrimSpace(level)) {
		case "", "auto", "enabled", "adaptive", "disabled", "none", "off":
		default:
			out = append(out, level)
		}
	}
	return out
}

// requestEffort resolves one request's reasoning depth: a vocabulary-approved
// EffortOverride wins, anything else falls back to the configured effort.
func (c *client) requestEffort(req provider.Request) string {
	want := strings.ToLower(strings.TrimSpace(req.EffortOverride))
	if want == "" || !supportsEffort(c.requestEfforts, want) {
		return c.effort
	}
	return want
}

func supportsEffort(levels []string, want string) bool {
	want = strings.ToLower(strings.TrimSpace(want))
	for _, level := range levels {
		if strings.ToLower(strings.TrimSpace(level)) == want {
			return true
		}
	}
	return false
}

func hasExplicitSupportedEfforts(levels []string) bool {
	for _, level := range levels {
		level = strings.ToLower(strings.TrimSpace(level))
		if level != "" && level != "auto" {
			return true
		}
	}
	return false
}
