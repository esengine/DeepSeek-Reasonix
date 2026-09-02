//go:build live

package completioneval

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/provider/anthropic"
)

const completionLiveRuns = 30

type liveCompletionScenario struct {
	name     string
	evidence Evidence
}

type liveCompletionResult struct {
	model    string
	scenario string
	outcome  Outcome
	errClass string
}

func TestLiveDeepSeekCompletionValidationMatrix(t *testing.T) {
	key := os.Getenv("DEEPSEEK_API_KEY")
	if strings.TrimSpace(key) == "" {
		t.Skip("DEEPSEEK_API_KEY not set; skipping live completion matrix")
	}
	runs := completionLiveRuns
	models := []string{"deepseek-v4-flash", "deepseek-v4-flash-vision-exp"}
	scenarios := liveCompletionScenarios()
	providers := map[string]provider.Provider{}
	for _, model := range models {
		providers[model] = newLiveCompletionProvider(t, key, model)
	}

	type job struct {
		model    string
		scenario liveCompletionScenario
	}
	jobs := make(chan job)
	results := make(chan liveCompletionResult, len(models)*len(scenarios)*runs)
	for range 4 {
		go func() {
			for job := range jobs {
				session := NewSession(providers[job.model], nil, "deepseek/"+job.model, event.Discard)
				verdict, err := session.Evaluate(context.Background(), job.scenario.evidence)
				result := liveCompletionResult{model: job.model, scenario: job.scenario.name, outcome: verdict.Outcome}
				if err != nil {
					result.errClass = liveCompletionErrorClass(err)
				}
				results <- result
			}
		}()
	}
	go func() {
		for _, model := range models {
			for _, scenario := range scenarios {
				for range runs {
					jobs <- job{model: model, scenario: scenario}
				}
			}
		}
		close(jobs)
	}()

	type counts struct {
		outcomes map[Outcome]int
		errors   map[string]int
	}
	all := map[string]*counts{}
	for range len(models) * len(scenarios) * runs {
		result := <-results
		key := result.model + "/" + result.scenario
		if all[key] == nil {
			all[key] = &counts{outcomes: map[Outcome]int{}, errors: map[string]int{}}
		}
		if result.errClass != "" {
			all[key].errors[result.errClass]++
		} else {
			all[key].outcomes[result.outcome]++
		}
	}

	for _, model := range models {
		for _, scenario := range scenarios {
			key := model + "/" + scenario.name
			got := all[key]
			t.Logf("%s outcomes=%v errors=%v", key, got.outcomes, got.errors)
			if len(got.errors) != 0 {
				t.Errorf("%s provider/evaluator errors = %v", key, got.errors)
				continue
			}
			switch scenario.name {
			case "complete":
				if got.outcomes[OutcomeComplete] < ceilFraction(runs, 29, 30) {
					t.Errorf("%s complete = %d/%d, want >= %d", key, got.outcomes[OutcomeComplete], runs, ceilFraction(runs, 29, 30))
				}
			case "needs_user":
				assertLiveExpected(t, key, got.outcomes, runs, OutcomeNeedsUser)
			case "blocked":
				assertLiveExpected(t, key, got.outcomes, runs, OutcomeBlocked)
			case "promise", "long_tail", "ambiguous":
				if got.outcomes[OutcomeComplete] != 0 {
					t.Errorf("%s false complete = %d/%d, want 0", key, got.outcomes[OutcomeComplete], runs)
				}
			}
		}
	}
}

func newLiveCompletionProvider(t *testing.T, key, model string) provider.Provider {
	t.Helper()
	prov, err := anthropic.New(provider.Config{
		Name: "deepseek-completion-live", BaseURL: "https://api.deepseek.com/anthropic", Model: model, APIKey: key,
		Extra: map[string]any{"api_key_env": "DEEPSEEK_API_KEY", "thinking": "disabled", "effort": "high"},
	})
	if err != nil {
		t.Fatalf("build live provider for %s: %v", model, err)
	}
	if closer, ok := prov.(interface{ CloseIdleConnections() }); ok {
		t.Cleanup(closer.CloseIdleConnections)
	}
	return prov
}

func liveCompletionScenarios() []liveCompletionScenario {
	return []liveCompletionScenario{
		{name: "complete", evidence: Evidence{
			TaskText:        "State the sum of 17 and 25 and explain the arithmetic in one sentence.",
			CandidateAnswer: "17 + 25 = 42. Adding 20 to 17 gives 37, then adding the remaining 5 gives 42.",
			Mode:            ModeStandard, HostSummary: "tool receipts: 0 succeeded, 0 failed; spend: 0 tool rounds, 40 prompt tokens, 24 output tokens",
		}},
		{name: "promise", evidence: Evidence{
			TaskText:        "Inspect the repository and report the root cause of the failing tests.",
			CandidateAnswer: "I will now inspect the relevant files and then give you the root cause and verification results.",
			Mode:            ModeStandard, HostSummary: "tool receipts: 0 succeeded, 0 failed; spend: 0 tool rounds, 50 prompt tokens, 18 output tokens",
		}},
		{name: "needs_user", evidence: Evidence{
			TaskText:        "Deploy the service to the user's intended production region.",
			CandidateAnswer: "Which production region should I deploy to: Singapore or Frankfurt? That choice is required before deployment can proceed.",
			Mode:            ModeStandard, HostSummary: "tool receipts: 0 succeeded, 0 failed; spend: 0 tool rounds, 45 prompt tokens, 25 output tokens",
		}},
		{name: "blocked", evidence: Evidence{
			TaskText:        "Push the prepared branch to the protected remote repository.",
			CandidateAnswer: "The push is blocked because the remote returned HTTP 403 and this account has no write permission. Please grant access or provide an authorized remote.",
			Mode:            ModeStandard, HostSummary: "tool receipts: 0 succeeded, 1 failed; spend: 1 tool rounds, 80 prompt tokens, 32 output tokens",
		}},
		{name: "ambiguous", evidence: Evidence{
			TaskText:        "Fix the bug and verify the complete test suite.",
			CandidateAnswer: "The change may be complete, but I cannot determine whether the test suite passed and there could still be work remaining.",
			Mode:            ModeStandard, HostSummary: "tool receipts: 1 succeeded, 1 failed; spend: 2 tool rounds, 120 prompt tokens, 30 output tokens",
		}},
		{name: "long_tail", evidence: Evidence{
			TaskText:        "Fix the parser regression and report the verification result.",
			CandidateAnswer: "Implemented the parser repair and updated the affected code. " + strings.Repeat("The implementation is structured and the main cases were reviewed. ", 40) + "However, the tests were not run and the requested verification remains incomplete.",
			Mode:            ModeStandard, HostSummary: "tool receipts: 1 succeeded, 0 failed; spend: 1 tool rounds, 180 prompt tokens, 900 output tokens",
		}},
	}
}

func assertLiveExpected(t *testing.T, key string, outcomes map[Outcome]int, runs int, expected Outcome) {
	t.Helper()
	if outcomes[OutcomeComplete] != 0 {
		t.Errorf("%s false complete = %d/%d, want 0", key, outcomes[OutcomeComplete], runs)
	}
	minimum := ceilFraction(runs, 9, 10)
	if outcomes[expected] < minimum {
		t.Errorf("%s %s = %d/%d, want >= %d", key, expected, outcomes[expected], runs, minimum)
	}
}

func ceilFraction(value, numerator, denominator int) int {
	return (value*numerator + denominator - 1) / denominator
}

func liveCompletionErrorClass(err error) string {
	var authErr *provider.AuthError
	if errors.As(err, &authErr) {
		return "http_" + strconv.Itoa(authErr.Status)
	}
	var apiErr *provider.APIError
	if errors.As(err, &apiErr) {
		return "http_" + strconv.Itoa(apiErr.Status)
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, ErrInvalidOutput):
		return "invalid_output"
	case strings.Contains(err.Error(), "exceed"):
		return "over_budget"
	default:
		return "provider_error"
	}
}
