package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/netclient"
	"reasonix/internal/repair"
)

func TestRepairPlanProviderConfigPreservesKimiK3Efforts(t *testing.T) {
	entry := &config.ProviderEntry{
		Name:              "opencode-go",
		Kind:              "openai",
		BaseURL:           "https://opencode.ai/zen/go/v1",
		Model:             "kimi-k3",
		ReasoningProtocol: config.ReasoningProtocolOpenAI,
		SupportedEfforts:  []string{"high", "max"},
		DefaultEffort:     "max",
	}
	got := repairPlanProviderConfig(entry, netclient.ProxySpec{})
	if effort := got.Extra["effort"]; effort != "max" {
		t.Fatalf("effort = %#v, want max", effort)
	}
	if efforts, ok := got.Extra["supported_efforts"].([]string); !ok || !reflect.DeepEqual(efforts, []string{"high", "max"}) {
		t.Fatalf("supported_efforts = %#v, want [high max]", got.Extra["supported_efforts"])
	}
	if protocol := got.Extra["reasoning_protocol"]; protocol != config.ReasoningProtocolOpenAI {
		t.Fatalf("reasoning_protocol = %#v, want openai", protocol)
	}
}

// TestProviderSafeReportDropsUserControlledContent pins the outbound-privacy
// contract: the payload sent to the AI provider must carry only allowlisted
// codes, severities, and generic scopes — never diagnostic prose, which can
// quote config lines, MCP command lines, permission rules, URLs with
// credentials, emails, tokens, or absolute paths.
func TestProviderSafeReportDropsUserControlledContent(t *testing.T) {
	const metadataSecret = "snapshot-metadata-secret"
	const snapshotID = "20260715T000000.000000000Z-abcdef123456"
	secrets := []string{
		"/Users/someone/dotfiles",
		"someone@example.com",
		"sk-live-Abc123Secret",
		"npx --api-key=sk-live-Abc123Secret mcp-server",
		"bash(rm -rf *)",
		"corp-internal-llm",
		"https://user:pass@proxy.internal:8080",
		metadataSecret,
		"secret.finding.code",
		"secret-severity",
		snapshotID,
	}
	report := repair.DiagnosticReport{
		GeneratedAt: metadataSecret,
		Root:        "/Users/someone/dotfiles",
		Network:     true,
		Snapshots: []repair.DiagnosticSnapshot{
			{ID: snapshotID, RecordedAt: metadataSecret, Version: metadataSecret},
			{ID: metadataSecret, RecordedAt: metadataSecret, Version: metadataSecret},
		},
		PendingUpdate: &repair.DiagnosticUpdate{FromVersion: metadataSecret, ToVersion: metadataSecret},
		Findings: []repair.DiagnosticFinding{
			{Severity: "error", Code: "config.invalid_toml", Scope: "global",
				Message: "Configuration cannot be parsed: line 3: api_key = \"sk-live-Abc123Secret\" contact someone@example.com"},
			{Severity: "warning", Code: "plugin.command_missing", Scope: "plugin:corp-internal-llm",
				Message: `MCP command "npx --api-key=sk-live-Abc123Secret mcp-server" was not found.`},
			{Severity: "warning", Code: "permissions.conflict", Scope: "permissions",
				Message: `Permission rule "bash(rm -rf *)" appears in allow, deny; deny takes precedence.`},
			{Severity: "error", Code: "network.invalid_proxy", Scope: "network",
				Message: "Proxy configuration is invalid: parse \"https://user:pass@proxy.internal:8080\": bad"},
			{Severity: "warning", Code: "provider.missing_key", Scope: "provider:corp-internal-llm",
				Message: "The configured API key is missing."},
			{Severity: "warning", Code: "derived.invalid_json", Scope: "derived:tabs",
				Message: "Derived desktop state desktop-tabs.json is malformed."},
			{Severity: "secret-severity", Code: "secret.finding.code", Scope: metadataSecret,
				Message: metadataSecret, Remediation: metadataSecret},
		},
	}

	safeReport, aliases := providerSafeReportFrom(report)
	payload, err := json.Marshal(safeReport)
	if err != nil {
		t.Fatal(err)
	}
	body := string(payload)
	for _, secret := range secrets {
		if strings.Contains(body, secret) {
			t.Fatalf("outbound payload leaked %q:\n%s", secret, body)
		}
	}
	for _, want := range []string{
		`"root":"\u003cproject\u003e"`, // json.Marshal HTML-escapes <project>
		`"code":"config.invalid_toml"`,
		`"scope":"provider"`,
		`"scope":"plugin"`,
		`"scope":"derived:tabs"`,
		`"id":"snapshot-1"`,
		`"id":"snapshot-2"`,
		`"pendingUpdate":true`,
		`"severity":"unknown"`,
		`"code":"unknown"`,
		`"scope":"other"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("outbound payload missing %s:\n%s", want, body)
		}
	}
	if aliases["snapshot-1"] != snapshotID || aliases["snapshot-2"] != metadataSecret {
		t.Fatalf("snapshot alias map = %v", aliases)
	}
	for _, forbiddenField := range []string{"generatedAt", "recordedAt", "version", "fromVersion", "toVersion"} {
		if strings.Contains(body, `"`+forbiddenField+`"`) {
			t.Fatalf("outbound payload retained free-form field %q:\n%s", forbiddenField, body)
		}
	}
}

func TestResolveProviderSnapshotAliases(t *testing.T) {
	const snapshotID = "20260715T000000.000000000Z-abcdef123456"
	plan, err := repair.DecodeRepairPlan([]byte(`{
		"schemaVersion": 1,
		"summary": "restore newest snapshot",
		"actions": [
			{"type":"repair_config","scope":"global","reason":"invalid config"},
			{"type":"restore_snapshot","snapshotId":"snapshot-1","reason":"known-good config"}
		]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveProviderSnapshotAliases(plan, map[string]string{"snapshot-1": snapshotID})
	if err != nil {
		t.Fatal(err)
	}
	if got := resolved.Actions[1].SnapshotID; got != snapshotID {
		t.Fatalf("resolved snapshot ID = %q, want %q", got, snapshotID)
	}
	if got := plan.Actions[1].SnapshotID; got != "snapshot-1" {
		t.Fatalf("alias resolution mutated source plan: %q", got)
	}
	if _, err := resolveProviderSnapshotAliases(plan, nil); err == nil {
		t.Fatal("unknown snapshot alias was accepted")
	}
}

func writeDerivedStatePlan(t *testing.T) (string, repair.RepairPlan) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "plan.json")
	raw := []byte(`{"schemaVersion":1,"summary":"rebuild tabs","actions":[{"type":"rebuild_derived_state","target":"tabs","reason":"malformed"}]}`)
	if err := os.WriteFile(file, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err := repair.DecodeRepairPlan(raw)
	if err != nil {
		t.Fatal(err)
	}
	return file, plan
}

func TestApplyPlanYesRequiresConfirmedPreviewID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	tabs := filepath.Join(home, "desktop-tabs.json")
	if err := os.WriteFile(tabs, []byte("bad-tabs"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, _ := writeDerivedStatePlan(t)
	if got := runApplyPlan([]string{"--file", file, "--yes"}); got != 2 {
		t.Fatalf("exit code = %d, want missing preview ID usage refusal", got)
	}
	if got, err := os.ReadFile(tabs); err != nil || string(got) != "bad-tabs" {
		t.Fatalf("unbound --yes touched derived state: %q, %v", got, err)
	}
}

func TestApplyPlanRejectsStateChangedAfterPreview(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	tabs := filepath.Join(home, "desktop-tabs.json")
	if err := os.WriteFile(tabs, []byte("first-state"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, plan := writeDerivedStatePlan(t)
	preview, err := repair.PreviewRepairPlan(plan, repair.ApplyPlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	previewID := repair.RepairPlanPreviewID(plan, preview)
	if err := os.WriteFile(tabs, []byte("changed-after-preview"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := runApplyPlan([]string{"--file", file, "--preview-id", previewID, "--yes"}); got != 1 {
		t.Fatalf("exit code = %d, want stale preview refusal", got)
	}
	if got, err := os.ReadFile(tabs); err != nil || string(got) != "changed-after-preview" {
		t.Fatalf("stale preview touched derived state: %q, %v", got, err)
	}
}

func TestApplyPlanAcceptsCurrentConfirmedPreviewID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	tabs := filepath.Join(home, "desktop-tabs.json")
	if err := os.WriteFile(tabs, []byte("bad-tabs"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, plan := writeDerivedStatePlan(t)
	preview, err := repair.PreviewRepairPlan(plan, repair.ApplyPlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	previewID := repair.RepairPlanPreviewID(plan, preview)
	if got := runApplyPlan([]string{"--file", file, "--preview-id", previewID, "--yes"}); got != 0 {
		t.Fatalf("exit code = %d, want confirmed preview application", got)
	}
	if _, err := os.Stat(tabs); !os.IsNotExist(err) {
		t.Fatalf("confirmed repair did not quarantine derived state: %v", err)
	}
}

func TestProviderSafeScopeClosedVocabulary(t *testing.T) {
	cases := map[string]string{
		"":                       "",
		"global":                 "global",
		"project":                "project",
		"derived:tabs":           "derived:tabs",
		"derived:zoom":           "derived:zoom",
		"derived:/etc/passwd":    "derived:other",
		"provider:corp-llm":      "provider",
		"plugin:internal-mcp":    "plugin",
		"credential file":        "credential file",
		"someone@example.com":    "other",
		"/Users/someone/project": "other",
	}
	for in, want := range cases {
		if got := providerSafeScope(in); got != want {
			t.Errorf("providerSafeScope(%q) = %q, want %q", in, got, want)
		}
	}
}
