package cli

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/i18n"
)

func TestTUIDiagnosticsKeepProcessAndPluginLogsOffTerminal(t *testing.T) {
	var terminal bytes.Buffer
	beforeTest := slog.Default()
	terminalLogger := slog.New(slog.NewTextHandler(&terminal, nil))
	slog.SetDefault(terminalLogger)
	t.Cleanup(func() { slog.SetDefault(beforeTest) })

	d := startTUIDiagnostics(t.TempDir())
	t.Cleanup(d.Close)
	slog.Warn("controller: snapshot conflict", "path", "private-session.jsonl")
	fmt.Fprintln(d.Writer(), "plugin diagnostic")
	if got := terminal.String(); got != "" {
		t.Fatalf("terminal received diagnostics while TUI owned it: %q", got)
	}

	logPath := d.path
	d.Close()
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read TUI diagnostic log: %v", err)
	}
	got := string(data)
	for _, want := range []string{"controller: snapshot conflict", "private-session.jsonl", "plugin diagnostic"} {
		if !strings.Contains(got, want) {
			t.Fatalf("diagnostic log = %q, want %q", got, want)
		}
	}

	slog.Warn("after TUI")
	if got := terminal.String(); !strings.Contains(got, "after TUI") {
		t.Fatalf("previous logger was not restored after TUI close: %q", got)
	}
}

func TestTUIDiagnosticsFallBackToDiscardWithoutLeakingToTerminal(t *testing.T) {
	var terminal bytes.Buffer
	beforeTest := slog.Default()
	terminalLogger := slog.New(slog.NewTextHandler(&terminal, nil))
	slog.SetDefault(terminalLogger)
	t.Cleanup(func() { slog.SetDefault(beforeTest) })

	blockedHome := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blockedHome, []byte("file"), 0o600); err != nil {
		t.Fatalf("seed blocked home: %v", err)
	}
	d := startTUIDiagnostics(blockedHome)
	defer d.Close()

	slog.Warn("must stay off terminal")
	fmt.Fprintln(d.Writer(), "plugin must stay off terminal")
	if got := terminal.String(); got != "" {
		t.Fatalf("fallback leaked diagnostics to terminal: %q", got)
	}
	if d.path != "" {
		t.Fatalf("fallback diagnostic path = %q, want empty", d.path)
	}
}

func TestBoundedDiagnosticWriterStopsAtLimit(t *testing.T) {
	var dst bytes.Buffer
	w := &boundedDiagnosticWriter{dst: &dst, remaining: 8}
	payload := strings.Repeat("x", 32)
	n, err := io.WriteString(w, payload)
	if err != nil || n != len(payload) {
		t.Fatalf("Write = (%d, %v), want (%d, nil)", n, err, len(payload))
	}
	if !strings.HasPrefix(dst.String(), strings.Repeat("x", 8)) {
		t.Fatalf("bounded output = %q, want eight payload bytes first", dst.String())
	}
	if !strings.Contains(dst.String(), "diagnostic log limit reached") {
		t.Fatalf("bounded output = %q, want truncation marker", dst.String())
	}
	before := dst.Len()
	if _, err := io.WriteString(w, "more"); err != nil {
		t.Fatalf("discard after cap: %v", err)
	}
	if dst.Len() != before {
		t.Fatalf("writer grew after cap: before=%d after=%d", before, dst.Len())
	}
}

func TestCLIProfileBuildOptionsPropagateInteractiveOwners(t *testing.T) {
	var diagnostic bytes.Buffer
	recovered := false
	onRecovered := func(control.SessionRecoveryInfo) error {
		recovered = true
		return nil
	}
	opts := cliProfileBuildOptions("provider/model", 9, false, event.Discard, "delivery", cliBuildOverrides{
		WorkspaceRoot:        "/workspace",
		HeadlessApprovalMode: control.ToolApprovalAuto,
		Stderr:               &diagnostic,
		OnSessionRecovered:   onRecovered,
	})

	if opts.Stderr != &diagnostic {
		t.Fatalf("Stderr = %T, want caller-owned diagnostic writer", opts.Stderr)
	}
	if opts.OnSessionRecovered == nil {
		t.Fatal("OnSessionRecovered was dropped from CLI build options")
	}
	if err := opts.OnSessionRecovered(control.SessionRecoveryInfo{RecoveryPath: "recovery.jsonl"}); err != nil {
		t.Fatalf("OnSessionRecovered: %v", err)
	}
	if !recovered {
		t.Fatal("propagated recovery callback was not invoked")
	}
	if opts.HeadlessApprovalMode != control.ToolApprovalAuto {
		t.Fatalf("HeadlessApprovalMode = %q, want %q", opts.HeadlessApprovalMode, control.ToolApprovalAuto)
	}
}

func TestCLIProfileBuildOptionsUseResolvedLocaleForAutoPricing(t *testing.T) {
	defer i18n.DetectLanguage("en")
	for _, tt := range []struct {
		language string
		want     string
	}{
		{language: "en", want: "USD"},
		{language: "zh", want: "CNY"},
		{language: "zh-TW", want: "CNY"},
	} {
		i18n.DetectLanguage(tt.language)
		opts := cliProfileBuildOptions("provider/model", 0, false, event.Discard, "balanced", cliBuildOverrides{})
		if opts.AutoPricingCurrency != tt.want {
			t.Errorf("language %q auto pricing currency = %q, want %q", tt.language, opts.AutoPricingCurrency, tt.want)
		}
	}
}
