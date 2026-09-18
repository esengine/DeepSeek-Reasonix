package agent

import (
	"sync"
	"testing"
)

// The measured Ollama case: a ~12,000-token prompt answered with HTTP 200 and
// prompt_tokens 2051. Nothing else in the response says the prompt was cut.
func TestPromptTruncationCeilingDetectsTheMeasuredOllamaCase(t *testing.T) {
	shape := requestCalibrationShape{requestChars: 54_000}
	if got := promptTruncationCeiling(2051, shape, nil); got != 2051 {
		t.Fatalf("ceiling = %d, want 2051", got)
	}
}

// The case the absolute floor alone misses: a density of 0.10 is a plausible
// tokenizer in isolation, so it is accepted and becomes the calibration. Against
// an established 0.26 it cannot be the same tokenizer on the same wire.
func TestPromptTruncationCeilingCatchesADropFromAnEstablishedDensity(t *testing.T) {
	cal := &promptTokenCalibration{promptTokens: 2600, requestChars: 10_000}
	shape := requestCalibrationShape{requestChars: 20_000}
	if got := promptTruncationCeiling(2051, shape, cal); got != 2051 {
		t.Fatalf("ceiling = %d, want 2051", got)
	}
}

func TestPromptTruncationCeilingLeavesHonestObservationsAlone(t *testing.T) {
	cal := &promptTokenCalibration{promptTokens: 2600, requestChars: 10_000}
	for _, tc := range []struct {
		name         string
		promptTokens int
		chars        int64
		cal          *promptTokenCalibration
	}{
		{"uncalibrated dense CJK", 4_000, 20_000, nil},
		{"calibrated steady", 5_200, 20_000, cal},
		{"calibrated mild drift", 3_200, 20_000, cal},
		{"request too short to judge", 200, 4_000, nil},
		{"no usage reported", 0, 20_000, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			shape := requestCalibrationShape{requestChars: tc.chars}
			if got := promptTruncationCeiling(tc.promptTokens, shape, tc.cal); got != 0 {
				t.Fatalf("ceiling = %d, want 0 (no truncation)", got)
			}
		})
	}
}

// The loop this closes: a truncated observation must never become the ratio
// every later estimate is built from. Refusing it is safe on first sight, so it
// does not wait for corroboration.
func TestTruncatedObservationDoesNotBecomeTheCalibration(t *testing.T) {
	a := &Agent{}
	shape := requestCalibrationShape{requestChars: 54_000}
	a.setPromptTokenCalibration(2051, shape)
	if cal := a.sess.output.promptCalibration.Load(); cal != nil {
		t.Fatalf("truncated observation was stored as calibration: %+v", cal)
	}
	if _, ok := a.calibratedPromptTokens(shape); ok {
		t.Fatal("estimates are calibrated from a truncated prompt")
	}
}

// Detection must never clamp the window: admission would then reject every
// prompt that does not fit, stopping a session that today merely degrades.
func TestTruncationNeverClampsTheWindow(t *testing.T) {
	a := &Agent{}
	a.setPromptTokenCalibration(2051, requestCalibrationShape{requestChars: 54_000})
	a.setPromptTokenCalibration(2048, requestCalibrationShape{requestChars: 90_000})
	if learned := a.sess.output.learned.Load(); learned != nil && learned.windowTokens > 0 {
		t.Fatalf("detection clamped the window to %d", learned.windowTokens)
	}
}

// The measured session truncates on its second turn, so the user must be told
// on that turn rather than after another round trip.
func TestTheFirstTruncatedTurnIsTheOneThatWarns(t *testing.T) {
	a := &Agent{}
	a.setPromptTokenCalibration(2051, requestCalibrationShape{requestChars: 54_000})
	tr := a.sess.output.truncation.Load()
	if tr == nil || !tr.notified || tr.promptCeiling != 2051 {
		t.Fatalf("truncation state = %+v, want notified ceiling 2051", tr)
	}
}

// A provider reporting meaningless usage is not a small window. Stub and broken
// providers report token counts no runtime could be serving.
func TestImplausiblySmallUsageIsNotTreatedAsACeiling(t *testing.T) {
	shape := requestCalibrationShape{requestChars: 200_000}
	if got := promptTruncationCeiling(10, shape, nil); got != 0 {
		t.Fatalf("ceiling = %d, want 0: 10 tokens is not a context window", got)
	}
}

func TestHonestObservationStillCalibrates(t *testing.T) {
	a := &Agent{}
	shape := requestCalibrationShape{requestChars: 20_000}
	a.setPromptTokenCalibration(5_200, shape)
	cal := a.sess.output.promptCalibration.Load()
	if cal == nil || cal.promptTokens != 5_200 {
		t.Fatalf("honest observation was rejected: %+v", cal)
	}
}

// A calibration that is itself implausible must not suppress detection: the
// absolute floor still applies underneath it.
func TestAnImplausibleCalibrationDoesNotSuppressDetection(t *testing.T) {
	cal := &promptTokenCalibration{promptTokens: 400, requestChars: 100_000} // density 0.004
	shape := requestCalibrationShape{requestChars: 54_000}
	if got := promptTruncationCeiling(2051, shape, cal); got != 2051 {
		t.Fatalf("ceiling = %d, want 2051", got)
	}
}

// One warning per session, not one per racing turn.
func TestConcurrentTurnsWarnOnlyOnce(t *testing.T) {
	a := &Agent{}
	shape := requestCalibrationShape{requestChars: 54_000}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() { a.setPromptTokenCalibration(2051, shape) })
	}
	wg.Wait()
	tr := a.sess.output.truncation.Load()
	if tr == nil || !tr.notified {
		t.Fatalf("truncation state = %+v, want notified", tr)
	}
}
