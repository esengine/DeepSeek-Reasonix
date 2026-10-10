package config

import (
	"errors"
	"testing"
)

func decisionPickConfig(def string) *Config {
	return &Config{
		DefaultModel: def,
		Providers: []ProviderEntry{
			{Name: "laya", Kind: "typesafe", BaseURL: "http://127.0.0.1:8700", Models: []string{"typed-decisions"}},
			{Name: "chat", Kind: "openai", BaseURL: "https://example.invalid/v1", Models: []string{"m1"}},
		},
	}
}

func TestRequireAnswersNamesTheMismatch(t *testing.T) {
	c := decisionPickConfig("chat/m1")
	var mismatch *AnswersMismatchError
	err := c.RequireAnswers("laya/typed-decisions", AnswersChat)
	if !errors.As(err, &mismatch) || mismatch.Has != AnswersDecision || mismatch.Want != AnswersChat {
		t.Fatalf("decision source asked to chat = %v", err)
	}
	if err := c.RequireAnswers("chat/m1", AnswersChat); err != nil {
		t.Fatalf("chat model asked to chat = %v", err)
	}
	if err := c.RequireAnswers("nonesuch", AnswersChat); err != nil {
		t.Fatalf("an unresolved ref is not a mismatch: %v", err)
	}
}

func TestANewSessionNeverOpensOnADecisionSource(t *testing.T) {
	for _, def := range []string{"laya/typed-decisions", "laya", ""} {
		for name, resolve := range map[string]func() (NewSessionModel, bool){
			"new session": decisionPickConfig(def).ResolveNewSessionChatModel,
			"startup":     decisionPickConfig(def).ResolveStartupChatModel,
		} {
			start, ok := resolve()
			if !ok || start.Ref != "chat/m1" || start.SkippedDefault != def {
				t.Fatalf("%s with default %q = %+v (ok=%v), want chat/m1 with %q reported as skipped", name, def, start, ok, def)
			}
		}
	}
}
