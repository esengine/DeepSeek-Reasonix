package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// Remedy names the setting whose change would lift a refusal. The producer that
// knows why the call cannot proceed sets it; the model reads it rendered by the
// host and never parses it out of a sentence.
type Remedy struct {
	// Op is RemedyPropose where the model may ask for the change, RemedyOpen where
	// only the user may make it.
	Op      string
	Setting string
	Value   json.RawMessage
}

const (
	RemedyPropose = "propose"
	RemedyOpen    = "open"
)

func (r Remedy) String() string {
	if r.Op == RemedyPropose && len(r.Value) > 0 {
		return fmt.Sprintf("tool:setting op=%s id=%s value=%s", r.Op, r.Setting, r.Value)
	}
	return fmt.Sprintf("tool:setting op=%s id=%s", r.Op, r.Setting)
}

// SettingInfo is one row of what the model may learn about a setting.
type SettingInfo struct {
	ID     string `json:"id"`
	Class  string `json:"class"`
	Effect string `json:"effect"`
	// Value is absent for a secret or a setting edited in its own screen.
	Value any `json:"value,omitempty"`
}

// SettingReceipt is what a confirmed change did.
type SettingReceipt struct {
	ID     string `json:"id"`
	From   any    `json:"from"`
	To     any    `json:"to"`
	Effect string `json:"effect"`
	// Via is "click" for a fresh answer and "session_grant" for one the user
	// gave earlier in the session for this setting.
	Via string `json:"via"`
}

// SettingsHost is the controller side of the setting tool.
type SettingsHost interface {
	SettingsList(query string) []SettingInfo
	ProposeSetting(ctx context.Context, id string, value json.RawMessage, reason string) (SettingReceipt, error)
	OpenSetting(id string) error
}

// SettingsLink joins the setting tool, built with the registry, to the
// controller that exists only afterwards.
type SettingsLink struct {
	mu   sync.RWMutex
	host SettingsHost
}

func (l *SettingsLink) Bind(h SettingsHost) {
	l.mu.Lock()
	l.host = h
	l.mu.Unlock()
}

func (l *SettingsLink) Host() (SettingsHost, bool) {
	if l == nil {
		return nil, false
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.host, l.host != nil
}
