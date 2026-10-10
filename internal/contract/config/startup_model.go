package config

import "strings"

// ResolveStartupChatModel is ResolveNewSessionChatModel for a surface that has
// to open even when default_model names nothing configured: that default is
// passed over like a keyless one and returned as skipped, so the caller can say
// so. With nothing to fall back to, the stale default is kept for the error.
func (c *Config) ResolveStartupChatModel() (resolvedRef, skippedDefault string, ok bool) {
	if c == nil {
		return "", "", false
	}
	ref, _, ok := c.resolveNewSessionChatModel(nil, false)
	def := strings.TrimSpace(c.DefaultModel)
	if def == "" {
		return ref, "", ok
	}
	if entry, found := c.ResolveModel(def); found && Answering(entry.Kind, AnswersChat) {
		return ref, "", ok
	}
	if ok {
		return ref, def, true
	}
	return def, "", true
}
