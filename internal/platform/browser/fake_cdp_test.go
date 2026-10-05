package browser

import (
	"context"
	"encoding/json"
	"testing"
)

// fakePage answers a tab's commands from a table, so page logic runs without
// a browser. calls records every method sent, in order.
type fakePage struct {
	nodes          map[int64][]string // backend node id → localName then attributes
	active         int64              // backend node id document.activeElement names; 0 is <body>
	evalExpression string
	evalContext    string
	evalResult     any
	evalRemote     map[string]any
	evalException  string
	calls          []string
	// answers, when set, is asked first and answers the methods it knows.
	answers func(msg wireMessage) (any, bool)
}

func newFakeSession(t *testing.T, page *fakePage) (*Session, *tab) {
	t.Helper()
	ft := newFakeTransport(func(msg wireMessage) []byte {
		page.calls = append(page.calls, msg.Method)
		result := page.answer(msg)
		raw, _ := json.Marshal(map[string]any{"id": msg.ID, "result": result})
		return raw
	})
	s := NewSession(Config{})
	eng := &engine{conn: newConn(ft), exited: make(chan struct{}), listeners: map[int]func(event){}}
	tb := newTab(s, eng, "t1", "target-1", "S1")
	s.eng, s.tabs, s.active = eng, []*tab{tb}, tb
	t.Cleanup(func() { eng.conn.shutdown() })
	return s, tb
}

func (p *fakePage) answer(msg wireMessage) any {
	if p.answers != nil {
		if out, ok := p.answers(msg); ok {
			return out
		}
	}
	var params struct {
		BackendNodeID int64  `json:"backendNodeId"`
		ObjectID      string `json:"objectId"`
		Expression    string `json:"expression"`
		UniqueContext string `json:"uniqueContextId"`
	}
	_ = json.Unmarshal(msg.Params, &params)
	switch msg.Method {
	case "Runtime.evaluate":
		if params.Expression == "document.activeElement" {
			return map[string]any{"result": map[string]any{"objectId": "active"}}
		}
		if params.Expression != "document.title" {
			p.evalExpression = params.Expression
			p.evalContext = params.UniqueContext
		}
		if p.evalException != "" {
			return map[string]any{
				"result":           map[string]any{"type": "object"},
				"exceptionDetails": map[string]any{"text": "Uncaught", "exception": map[string]any{"description": p.evalException}},
			}
		}
		if p.evalResult != nil {
			raw, _ := json.Marshal(p.evalResult)
			return map[string]any{"result": map[string]any{"type": "string", "value": string(raw)}}
		}
		if p.evalRemote != nil {
			return map[string]any{"result": p.evalRemote}
		}
		return map[string]any{"result": map[string]any{"value": ""}}
	case "DOM.describeNode":
		node := params.BackendNodeID
		if params.ObjectID == "active" {
			node = p.active
		}
		desc, ok := p.nodes[node]
		if !ok {
			desc = []string{"body"}
		}
		return map[string]any{"node": map[string]any{"localName": desc[0], "attributes": desc[1:]}}
	}
	return map[string]any{}
}

func TestCredentialEntryFollowsFocusThroughTheSteps(t *testing.T) {
	page := &fakePage{nodes: map[int64][]string{
		1: {"input", "type", "email"},
		2: {"input", "type", "password"},
		3: {"iframe"},
	}}
	s, _ := newFakeSession(t, page)
	email, password, frame := s.refs.refFor("t1", 1), s.refs.refFor("t1", 2), s.refs.refFor("t1", 3)
	ctx := context.Background()
	cases := []struct {
		name  string
		steps []Step
		want  bool
	}{
		{"email", []Step{{Action: "fill", Ref: email, Text: "a"}}, false},
		{"password", []Step{{Action: "fill", Ref: password, Text: "a"}}, true},
		{"a frame", []Step{{Action: "type", Ref: frame, Text: "a"}}, true},
		{"after clicking the password", []Step{{Action: "click", Ref: password}, {Action: "type", Text: "a"}}, true},
		{"after clicking the email", []Step{{Action: "click", Ref: email}, {Action: "type", Text: "a"}}, false},
		{"after Tab", []Step{{Action: "fill", Ref: email, Text: "a"}, {Action: "press", Key: "Tab"}, {Action: "type", Text: "b"}}, true},
		{"after a coordinate click", []Step{{Action: "click", X: new(1.0), Y: new(1.0)}, {Action: "type", Text: "b"}}, true},
		{"into the body", []Step{{Action: "type", Text: "b"}}, false},
		{"no typing", []Step{{Action: "click", Ref: password}, {Action: "press", Key: "Enter"}}, false},
	}
	for _, tc := range cases {
		if got := s.CredentialEntry(ctx, "", tc.steps); got != tc.want {
			t.Errorf("%s: CredentialEntry = %v, want %v", tc.name, got, tc.want)
		}
	}
	page.active = 2
	if !s.CredentialEntry(ctx, "", []Step{{Action: "type", Text: "b"}}) {
		t.Error("typing where focus already sits on the password field was not a credential entry")
	}
	if s.CredentialEntry(ctx, "t9", []Step{{Action: "fill", Ref: password}}) {
		t.Error("a tab that does not exist reported a credential entry")
	}
}

func TestUnconfirmedSecretIsRefusedBeforeAnyInput(t *testing.T) {
	page := &fakePage{nodes: map[int64][]string{2: {"input", "autocomplete", "cc-number"}}}
	s, _ := newFakeSession(t, page)
	card := s.refs.refFor("t1", 2)
	res, err := s.Act(context.Background(), "", []Step{{Action: "fill", Ref: card, Text: "4242"}}, false, "")
	if CodeOf(err) != CodeUnconfirmedSecret || res.FailedAt != 0 {
		t.Fatalf("Act = %+v, %v; want %s at step 0", res, err, CodeUnconfirmedSecret)
	}
	for _, m := range page.calls {
		if m == "Input.insertText" || m == "DOM.focus" {
			t.Fatalf("the refused step still sent %s", m)
		}
	}
}

func TestSecretChecksAnswerForTheArgumentsTheyJudged(t *testing.T) {
	s := NewSession(Config{})
	a, b := []byte(`{"steps":[1]}`), []byte(`{"steps":[2]}`)
	s.NoteSecretCheck(a, true)
	s.NoteSecretCheck(b, false)
	if !s.SecretsConfirmed(a) || s.SecretsConfirmed(b) || s.SecretsConfirmed([]byte(`{}`)) {
		t.Fatal("secret checks answered for the wrong arguments")
	}
	for i := range 40 {
		s.NoteSecretCheck([]byte{byte(i)}, true)
	}
	if len(s.checks.secrets) > 33 {
		t.Fatalf("secret checks grew to %d entries", len(s.checks.secrets))
	}
}
