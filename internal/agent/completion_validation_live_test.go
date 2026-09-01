//go:build live

package agent

import (
	"errors"
	"os"
	"strings"
	"testing"

	"reasonix/internal/completioneval"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/provider/anthropic"
)

func TestLiveCompletionValidatorBoundsHostContinuation(t *testing.T) {
	key := os.Getenv("DEEPSEEK_API_KEY")
	if strings.TrimSpace(key) == "" {
		t.Skip("DEEPSEEK_API_KEY not set; skipping live host continuation gate")
	}
	for _, model := range []string{"deepseek-v4-flash", "deepseek-v4-flash-vision-exp"} {
		t.Run(model, func(t *testing.T) {
			prov, err := anthropic.New(provider.Config{
				Name: "deepseek-completion-live", BaseURL: "https://api.deepseek.com/anthropic", Model: model, APIKey: key,
				Extra: map[string]any{"api_key_env": "DEEPSEEK_API_KEY", "thinking": "disabled", "effort": "high"},
			})
			if err != nil {
				t.Fatalf("build live provider: %v", err)
			}
			if closer, ok := prov.(interface{ CloseIdleConnections() }); ok {
				t.Cleanup(closer.CloseIdleConnections)
			}
			evaluator := completioneval.NewSession(prov, nil, "deepseek/"+model, event.Discard)
			sink := &recordingSink{}
			_, runErr := runWithValidator(t, CompletionValidationEnforce, evaluator, [][]provider.Chunk{
				textTurn("I am about to inspect the repository and will provide the result afterward."),
				textTurn("I will continue the inspection now and give the conclusion in my next response."),
			}, sink)
			var pause *CompletionUncertainError
			if !errors.As(runErr, &pause) || pause.Cause != CompletionUncertainValidatorContinue {
				t.Fatalf("Run error = %v, want bounded second-continue pause", runErr)
			}
			records := sink.completionValidations()
			if len(records) != 2 || records[0].Outcome != "continue" || records[1].Outcome != "continue" {
				t.Fatalf("validation events = %+v, want two continue verdicts", records)
			}
		})
	}
}
