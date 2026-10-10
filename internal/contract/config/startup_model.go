package config

import "strings"

// NewSessionModel is the chat model a new session opens on. SkippedDefault is
// the saved default_model it passed over because that names no conversation
// source, so the caller can say why the session is not on it.
type NewSessionModel struct {
	Ref            string
	Fallback       bool
	SkippedDefault string
}

// ResolveStartupChatModel is ResolveNewSessionChatModel for a surface that has
// to open even when default_model names nothing configured: that default is
// passed over like a keyless one and reported as skipped, so the caller can say
// so. With nothing to fall back to, the stale default is kept for the error.
func (c *Config) ResolveStartupChatModel() (NewSessionModel, bool) {
	if c == nil {
		return NewSessionModel{}, false
	}
	ref, fallback, ok := c.resolveNewSessionChatModel(nil, false)
	if !ok && c.passesOverDefault() {
		return NewSessionModel{Ref: strings.TrimSpace(c.DefaultModel)}, true
	}
	return c.newSessionModel(ref, fallback), ok
}

// newSessionModel reports the saved default a resolution passed over: one that
// names no conversation source and is not the model it chose.
func (c *Config) newSessionModel(ref string, fallback bool) NewSessionModel {
	m := NewSessionModel{Ref: ref, Fallback: fallback}
	if def := strings.TrimSpace(c.DefaultModel); ref != def && c.passesOverDefault() {
		m.SkippedDefault = def
	}
	return m
}

// passesOverDefault reports a saved default_model that names no conversation
// source: nothing configured serves it, or the source it names answers
// something else.
func (c *Config) passesOverDefault() bool {
	def := strings.TrimSpace(c.DefaultModel)
	if def == "" {
		return false
	}
	entry, found := c.ResolveModel(def)
	return !found || !Answering(entry.Kind, AnswersChat)
}
