package cli

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/platform/telemetry"
)

type recordedStart struct {
	opts       telemetry.Options
	savedAtRun string
}

func captureTUITelemetryStart(t *testing.T) *[]recordedStart {
	t.Helper()
	previous := startCLITelemetryReporter
	t.Cleanup(func() { startCLITelemetryReporter = previous })
	var starts []recordedStart
	startCLITelemetryReporter = func(opts telemetry.Options) *telemetry.Reporter {
		saved, _ := config.LoadForEditReadOnlyStrict(config.UserConfigPath())
		starts = append(starts, recordedStart{opts: opts, savedAtRun: saved.Telemetry.CLIMetrics})
		return &telemetry.Reporter{}
	}
	return &starts
}

func TestTUIFirstRunAsksBeforeAnyTelemetryStarts(t *testing.T) {
	isolateCLIConfigHome(t)
	clearCLITelemetryPolicyEnv(t)
	t.Cleanup(func() { i18n.DetectLanguage("en") })
	i18n.DetectLanguage("en")
	starts := captureTUITelemetryStart(t)

	var out, errOut bytes.Buffer
	reporter := startTUITelemetry(config.Default(), "v2.31.0", "balanced", "auto", "fresh", strings.NewReader("y\n"), &out, &errOut)
	if reporter == nil || len(*starts) != 1 {
		t.Fatalf("reporter = %v, starts = %d", reporter, len(*starts))
	}
	got := (*starts)[0]
	if got.savedAtRun != "auto" {
		t.Fatalf("telemetry started with saved mode %q, before the answer was stored", got.savedAtRun)
	}
	if !strings.Contains(out.String(), "[Y/n]") || !strings.Contains(out.String(), "crash.reasonix.io") {
		t.Fatalf("prompt = %q", out.String())
	}
	o := got.opts
	if !o.Interactive || o.CLIMode != "tui" || o.Version != "v2.31.0" || o.Profile != "balanced" || o.PermissionMode != "auto" || o.SessionMode != "fresh" {
		t.Fatalf("options = %+v", o)
	}
}

func TestTUIDeclineStartsNothingAndStoresOff(t *testing.T) {
	isolateCLIConfigHome(t)
	clearCLITelemetryPolicyEnv(t)
	starts := captureTUITelemetryStart(t)

	var out, errOut bytes.Buffer
	if r := startTUITelemetry(config.Default(), "v2.31.0", "", "", "fresh", strings.NewReader("n\n"), &out, &errOut); r != nil || len(*starts) != 0 {
		t.Fatalf("declined session started telemetry: %v, %d", r, len(*starts))
	}
	saved, err := config.LoadForEditReadOnlyStrict(config.UserConfigPath())
	if err != nil || saved.CLITelemetryMode() != "off" {
		t.Fatalf("saved = %q, %v", saved.CLITelemetryMode(), err)
	}
}

func TestTUIKeepsAMode1xAlreadyStoredWithoutAsking(t *testing.T) {
	isolateCLIConfigHome(t)
	clearCLITelemetryPolicyEnv(t)
	starts := captureTUITelemetryStart(t)
	cfg := config.Default()
	cfg.Telemetry.CLIMetrics = "auto"

	var out bytes.Buffer
	startTUITelemetry(cfg, "v2.31.0", "", "", "fresh", strings.NewReader("n\n"), &out, &out)
	if out.Len() != 0 || len(*starts) != 1 {
		t.Fatalf("stored consent was asked again: %q, starts=%d", out.String(), len(*starts))
	}

	cfg.Telemetry.CLIMetrics = "off"
	startTUITelemetry(cfg, "v2.31.0", "", "", "fresh", strings.NewReader("y\n"), &out, &out)
	last := (*starts)[len(*starts)-1].opts
	if out.Len() != 0 || telemetry.Enabled(last.Mode, last.Version, last.Interactive) {
		t.Fatalf("a stored off asked or reported: %q %+v", out.String(), last)
	}
}

func TestTUIPolicyRefusalsNeitherPromptNorReport(t *testing.T) {
	for name, set := range map[string]func(*testing.T){
		"do not track": func(t *testing.T) { t.Setenv("DO_NOT_TRACK", "1") },
		"env off":      func(t *testing.T) { t.Setenv("REASONIX_TELEMETRY", "0") },
		"ci":           func(t *testing.T) { t.Setenv("CI", "true") },
	} {
		t.Run(name, func(t *testing.T) {
			isolateCLIConfigHome(t)
			clearCLITelemetryPolicyEnv(t)
			set(t)
			starts := captureTUITelemetryStart(t)
			var out bytes.Buffer
			startTUITelemetry(config.Default(), "v2.31.0", "", "", "fresh", strings.NewReader("y\n"), &out, &out)
			if out.Len() != 0 {
				t.Fatalf("prompted: %q", out.String())
			}
			for _, s := range *starts {
				if telemetry.Enabled(s.opts.Mode, s.opts.Version, s.opts.Interactive) {
					t.Fatal("reporting was enabled")
				}
			}
		})
	}
	t.Run("dev build", func(t *testing.T) {
		isolateCLIConfigHome(t)
		clearCLITelemetryPolicyEnv(t)
		captureTUITelemetryStart(t)
		var out bytes.Buffer
		startTUITelemetry(config.Default(), "dev", "", "", "fresh", strings.NewReader("y\n"), &out, &out)
		if out.Len() != 0 {
			t.Fatalf("dev build prompted: %q", out.String())
		}
	})
}

type collectSink struct{ got []event.Event }

func (s *collectSink) Emit(e event.Event) { s.got = append(s.got, e) }

func TestTUISinkWithoutReporterIsTheInnerSink(t *testing.T) {
	inner := &collectSink{}
	sink := tuiSink(inner, nil)
	sink.Emit(event.Event{Kind: event.TurnStarted})
	if len(inner.got) != 1 {
		t.Fatalf("events = %d", len(inner.got))
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("terminal gone") }

func TestConsentPromptThatEndsUnansweredStoresAndSendsNothing(t *testing.T) {
	for name, in := range map[string]io.Reader{
		"ctrl-d at the prompt": strings.NewReader(""),
		"closed stdin":         iotestEOF{},
		"read error":           failingReader{},
		"text then eof":        strings.NewReader("maybe"),
	} {
		t.Run(name, func(t *testing.T) {
			isolateCLIConfigHome(t)
			clearCLITelemetryPolicyEnv(t)
			starts := captureTUITelemetryStart(t)
			cfg := config.Default()
			var out, errOut bytes.Buffer
			if r := startTUITelemetry(cfg, "v2.31.0", "", "", "fresh", in, &out, &errOut); r != nil || len(*starts) != 0 {
				t.Fatalf("unanswered prompt started telemetry: %v, %d", r, len(*starts))
			}
			if cfg.CLITelemetryConfigured() {
				t.Fatalf("unanswered prompt stored %q", cfg.CLITelemetryMode())
			}
			saved, _ := config.LoadForEditReadOnlyStrict(config.UserConfigPath())
			if saved.CLITelemetryConfigured() {
				t.Fatalf("unanswered prompt wrote %q to disk", saved.Telemetry.CLIMetrics)
			}
		})
	}
}

type iotestEOF struct{}

func (iotestEOF) Read([]byte) (int, error) { return 0, io.EOF }

func TestConsentPromptEmptyLineKeepsTheDocumentedDefault(t *testing.T) {
	isolateCLIConfigHome(t)
	clearCLITelemetryPolicyEnv(t)
	starts := captureTUITelemetryStart(t)
	cfg := config.Default()
	var out bytes.Buffer
	startTUITelemetry(cfg, "v2.31.0", "", "", "fresh", strings.NewReader("\n"), &out, &out)
	if len(*starts) != 1 || cfg.CLITelemetryMode() != "auto" || !cfg.CLITelemetryConfigured() {
		t.Fatalf("empty line: starts=%d mode=%q", len(*starts), cfg.CLITelemetryMode())
	}
}

func TestAskAnswerReportsEndOfInputApartFromAnEmptyAnswer(t *testing.T) {
	var out bytes.Buffer
	if v, err := askAnswer(bufio.NewScanner(strings.NewReader("")), &out, "q", "Y/n"); !errors.Is(err, io.EOF) || v != "" {
		t.Fatalf("eof = %q, %v", v, err)
	}
	if v, err := askAnswer(bufio.NewScanner(strings.NewReader("\n")), &out, "q", "Y/n"); err != nil || v != "Y/n" {
		t.Fatalf("empty = %q, %v", v, err)
	}
	if got := ask(bufio.NewScanner(strings.NewReader("")), &out, "q", "y/N"); got != "y/N" {
		t.Fatalf("ask on eof = %q, want its default kept for non-consent callers", got)
	}
}
