package boot

import (
	"fmt"
	"strings"

	"reasonix/internal/config"
)

// appendModelIdentity folds the session's model identity and input modality
// into the cache-stable system prefix. A model cannot introspect which
// provider instance or SKU it is running as, and vision-capable SKUs kept
// answering "I am a text-only model" — then routed image work through MCP
// vision bridges or OCR instead of reading attached images directly (#9288).
//
// The section derives only from config-resolved entries (ref + EffectiveVision
// + context window), so within one session it is byte-identical every boot
// assembly and cannot churn the provider prefix cache the way a live probe
// would. Vision wording is behavioral: it tells the agent to read attached
// images directly and treat bridges/OCR as opt-in fallbacks, not defaults.
func appendModelIdentity(prompt string, e *config.ProviderEntry) string {
	if e == nil || strings.TrimSpace(e.Model) == "" {
		return prompt
	}
	ref := e.Name + "/" + e.Model
	var b strings.Builder
	b.WriteString(fmt.Sprintf("## Model\n\nYou are running as `%s`", ref))
	if window := e.ContextWindow; window > 0 {
		b.WriteString(fmt.Sprintf(" with a %d-token context window", window))
	}
	if config.EffectiveVision(e) {
		b.WriteString(".\nYou accept images as direct input: when the user attaches or pastes an\nimage (including screenshots arriving with a plain description of a\nproblem), read the image itself first and answer from what you see. Treat\nexternal vision bridges (MCP servers) and OCR tools as opt-in fallbacks for\nwhen direct viewing is not enough — do not route image understanding through\nthem by default, and never state that you cannot see images.\n")
	} else {
		b.WriteString(".\nYou are a text-only model: you cannot see image content. When the user\nattaches an image, say so plainly and ask for a description, or use a\nconfigured vision/OCR tool when one is available — do not claim to have\nread the image.\n")
	}
	return prompt + "\n\n" + b.String()
}
