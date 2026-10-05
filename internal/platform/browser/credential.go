package browser

import (
	"context"
	"strings"
)

// secretAutocomplete are the autocomplete tokens that mark a field holding a
// secret: a password, a one-time code, or payment card details.
var secretAutocomplete = map[string]bool{
	"current-password": true, "new-password": true, "one-time-code": true,
	"cc-number": true, "cc-csc": true, "cc-exp": true, "cc-exp-month": true, "cc-exp-year": true,
}

// CredentialEntry reports whether steps would type into a field that holds a
// secret, judged from the page's markup: a password input, or a field whose
// autocomplete names a secret. Typing into a frame, or into whatever has focus
// after a step moved it where the host cannot follow, counts as well, since
// nothing there can be inspected.
func (s *Session) CredentialEntry(ctx context.Context, tabID string, steps []Step) bool {
	t, err := s.tab(tabID)
	if err != nil {
		return false
	}
	focus, known := "", true
	for _, step := range steps {
		switch strings.ToLower(strings.TrimSpace(step.Action)) {
		case "click", "double_click":
			focus, known = step.Ref, step.Ref != ""
		case "press":
			if def, _, ok := parseKey(step.Key); !ok || def.key == "Tab" {
				focus, known = "", false
			}
		case "fill", "type":
			target := step.Ref
			if target == "" {
				if !known {
					return true
				}
				target = focus
			}
			if s.holdsSecret(ctx, t, target) {
				return true
			}
			if step.Ref != "" {
				focus, known = step.Ref, true
			}
		}
	}
	return false
}

// holdsSecret inspects a ref, or the focused element when ref is empty.
func (s *Session) holdsSecret(ctx context.Context, t *tab, ref string) bool {
	params := map[string]any{}
	if ref != "" {
		target, err := s.refs.resolve(ref)
		if err != nil || target.tab != t.id {
			return false
		}
		params["backendNodeId"] = target.node
	} else {
		var active struct {
			Result struct {
				ObjectID string `json:"objectId"`
			} `json:"result"`
		}
		if t.call(ctx, "Runtime.evaluate", map[string]any{"expression": "document.activeElement", "objectGroup": objectGroup}, &active) != nil || active.Result.ObjectID == "" {
			return false
		}
		params["objectId"] = active.Result.ObjectID
	}
	var d struct {
		Node struct {
			LocalName  string   `json:"localName"`
			Attributes []string `json:"attributes"`
		} `json:"node"`
	}
	if t.call(ctx, "DOM.describeNode", params, &d) != nil {
		return false
	}
	return secretMarkup(d.Node.LocalName, d.Node.Attributes)
}

func secretMarkup(localName string, attributes []string) bool {
	switch localName {
	case "iframe", "frame":
		return true
	}
	for i := 0; i+1 < len(attributes); i += 2 {
		name, value := attributes[i], strings.ToLower(attributes[i+1])
		switch name {
		case "type":
			if localName == "input" && value == "password" {
				return true
			}
		case "autocomplete":
			for token := range strings.FieldsSeq(value) {
				if secretAutocomplete[token] {
					return true
				}
			}
		}
	}
	return false
}

// NoteSecretCheck records what the permission check decided about one call's
// arguments. The call runs only after that check allowed it, so the verdict it
// was allowed under is what execution may act on.
func (s *Session) NoteSecretCheck(args []byte, secret bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.checks.secrets == nil || len(s.checks.secrets) > 32 {
		s.checks.secrets = map[string]bool{}
	}
	s.checks.secrets[string(args)] = secret
}

// SecretsConfirmed reports whether a call's arguments were allowed as entering
// a secret. A call no check has seen was not.
func (s *Session) SecretsConfirmed(args []byte) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.checks.secrets[string(args)]
}

// NoteScriptOrigin binds an evaluated script to the origin its permission covered.
func (s *Session) NoteScriptOrigin(args []byte, origin string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.checks.scriptOrigin == nil || len(s.checks.scriptOrigin) > 32 {
		s.checks.scriptOrigin = map[string]string{}
	}
	s.checks.scriptOrigin[string(args)] = origin
}

// ScriptOrigin returns the origin approved for a call's arbitrary JavaScript.
func (s *Session) ScriptOrigin(args []byte) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.checks.scriptOrigin[string(args)]
}
