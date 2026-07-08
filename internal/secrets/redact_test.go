package secrets

import (
	"strings"
	"testing"

	"reasonix/internal/provider"
)

func TestRedactMasksCommonSecretShapes(t *testing.T) {
	in := strings.Join([]string{
		"DEEPSEEK_API_KEY=sk-real-secret-value-123456",
		"Authorization: Bearer ghp_abcdefghijklmnopqrstuvwxyz",
		"token xoxb-123456789012-abcdefabcdef",
		"jwt eyJabc.def.ghi",
	}, "\n")

	got := Redact(in)
	for _, leaked := range []string{
		"sk-real-secret-value-123456",
		"ghp_abcdefghijklmnopqrstuvwxyz",
		"xoxb-123456789012-abcdefabcdef",
		"eyJabc.def.ghi",
	} {
		if strings.Contains(got, leaked) {
			t.Fatalf("secret leaked %q in:\n%s", leaked, got)
		}
	}
	for _, want := range []string{"DEEPSEEK_API_KEY=sk-rea", "Authorization: Bearer [redacted]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("redacted output missing %q:\n%s", want, got)
		}
	}
}

func TestRedactMasksNonBearerAuthorizationSchemes(t *testing.T) {
	in := strings.Join([]string{
		"Authorization: Basic dXNlcjpwYXNzd29yZA==",
		"Proxy-Authorization: Digest username-hash-abcdef0123456789",
		"Authorization: dXNlcjpwYXNzd29yZC1yYXc=",
	}, "\n")
	got := Redact(in)
	for _, leaked := range []string{"dXNlcjpwYXNzd29yZA==", "username-hash-abcdef0123456789", "dXNlcjpwYXNzd29yZC1yYXc="} {
		if strings.Contains(got, leaked) {
			t.Fatalf("authorization credential leaked %q:\n%s", leaked, got)
		}
	}
	for _, want := range []string{"Authorization: Basic [redacted]", "Digest [redacted]", "Authorization: [redacted]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("redacted output missing %q:\n%s", want, got)
		}
	}
	if again := Redact(got); again != got {
		t.Fatalf("authorization redaction not idempotent:\nonce:  %q\ntwice: %q", got, again)
	}
}

func TestRedactIsIdempotent(t *testing.T) {
	// The session save path re-redacts loaded (already-redacted) transcripts;
	// digest stability across load/save cycles requires a byte-for-byte no-op.
	in := strings.Join([]string{
		"DEEPSEEK_API_KEY=sk-real-secret-value-123456",
		"Authorization: Bearer ghp_abcdefghijklmnopqrstuvwxyz",
		"DB_PWD='hunter2-swordfish'",
		"plain text with PWD=/home/user/project untouched",
	}, "\n")
	once := Redact(in)
	twice := Redact(once)
	if once != twice {
		t.Fatalf("Redact not idempotent:\nonce:  %q\ntwice: %q", once, twice)
	}
}

func TestRedactLeavesWorkingDirectoryPWDAlone(t *testing.T) {
	in := "PWD=/home/user/project\nOLDPWD=/home/user\nDB_PWD=hunter2-swordfish-123"
	got := Redact(in)
	if !strings.Contains(got, "PWD=/home/user/project") {
		t.Fatalf("POSIX PWD variable was mangled:\n%s", got)
	}
	if !strings.Contains(got, "OLDPWD=/home/user") {
		t.Fatalf("OLDPWD was mangled:\n%s", got)
	}
	if strings.Contains(got, "hunter2-swordfish-123") {
		t.Fatalf("DB_PWD value leaked:\n%s", got)
	}
}

func TestEnvKeySensitive(t *testing.T) {
	sensitive := []string{"DEEPSEEK_API_KEY", "GH_TOKEN", "AWS_SECRET_ACCESS_KEY", "DB_PASSWORD", "MYSQL_PWD", "NPM_TOKEN"}
	for _, key := range sensitive {
		if !EnvKeySensitive(key) {
			t.Errorf("EnvKeySensitive(%q) = false, want true", key)
		}
	}
	benign := []string{"PWD", "OLDPWD", "PATH", "HOME", "LANG", "GOPATH", "TERM"}
	for _, key := range benign {
		if EnvKeySensitive(key) {
			t.Errorf("EnvKeySensitive(%q) = true, want false", key)
		}
	}
}

func TestFilterEnvDropsSensitiveKeys(t *testing.T) {
	got := FilterEnv([]string{
		"PATH=/usr/bin",
		"DEEPSEEK_API_KEY=sk-real-secret-value-123456",
		"GH_TOKEN=ghp_abcdefghijklmnopqrstuvwxyz",
		"PWD=/home/user/project",
		"HOME=/tmp/home",
	})
	joined := strings.Join(got, "\n")
	if strings.Contains(joined, "DEEPSEEK_API_KEY") || strings.Contains(joined, "GH_TOKEN") {
		t.Fatalf("sensitive env survived:\n%s", joined)
	}
	for _, want := range []string{"PATH=/usr/bin", "HOME=/tmp/home", "PWD=/home/user/project"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("non-sensitive env %q dropped:\n%s", want, joined)
		}
	}
}

func TestProcessEnvUnfilteredByDefault(t *testing.T) {
	t.Setenv("REASONIX_TEST_SECRET_TOKEN", "ghp_abcdefghijklmnopqrstuvwxyz")
	joined := strings.Join(ProcessEnv(), "\n")
	if !strings.Contains(joined, "REASONIX_TEST_SECRET_TOKEN=ghp_abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("ProcessEnv filtered by default; filter_subprocess_env must be opt-in:\n%s", joined)
	}

	SetFilterSubprocessEnv(true)
	t.Cleanup(func() { SetFilterSubprocessEnv(false) })
	joined = strings.Join(ProcessEnv(), "\n")
	if strings.Contains(joined, "REASONIX_TEST_SECRET_TOKEN") {
		t.Fatalf("ProcessEnv leaked sensitive key with filtering enabled:\n%s", joined)
	}
}

func TestRedactToolOutputHonorsToggle(t *testing.T) {
	const in = "DEEPSEEK_API_KEY=sk-real-secret-value-123456"
	if got := RedactToolOutput(in); strings.Contains(got, "sk-real-secret-value-123456") {
		t.Fatalf("tool output not redacted by default:\n%s", got)
	}
	SetRedactToolOutput(false)
	t.Cleanup(func() { SetRedactToolOutput(true) })
	if got := RedactToolOutput(in); got != in {
		t.Fatalf("RedactToolOutput altered output with the toggle off:\n%s", got)
	}
	// The durable-surface entry point ignores the toggle.
	if got := Redact(in); strings.Contains(got, "sk-real-secret-value-123456") {
		t.Fatalf("Redact must stay active regardless of the toggle:\n%s", got)
	}
}

func TestRedactMessagesDoesNotMutateInput(t *testing.T) {
	const secret = "sk-real-secret-value-123456"
	msgs := []provider.Message{
		{
			Role:    provider.RoleAssistant,
			Content: "checking",
			ToolCalls: []provider.ToolCall{
				{ID: "call_1", Name: "bash", Arguments: `{"command":"echo DEEPSEEK_API_KEY=` + secret + `"}`},
			},
			MemoryCitations: []provider.MemoryCitation{{Note: "token " + secret}},
		},
		{Role: provider.RoleTool, ToolCallID: "call_1", Content: "DEEPSEEK_API_KEY=" + secret},
	}

	out := RedactMessages(msgs)

	// The redacted copy must not carry the raw secret...
	if strings.Contains(out[0].ToolCalls[0].Arguments, secret) || strings.Contains(out[1].Content, secret) {
		t.Fatalf("redacted copy leaked secret: %+v", out)
	}
	// ...and the input — live session history the model still replays — must
	// be untouched, including through the shared ToolCalls/MemoryCitations
	// backing arrays.
	if !strings.Contains(msgs[0].ToolCalls[0].Arguments, secret) {
		t.Fatalf("RedactMessages mutated the caller's ToolCalls: %q", msgs[0].ToolCalls[0].Arguments)
	}
	if !strings.Contains(msgs[0].MemoryCitations[0].Note, secret) {
		t.Fatalf("RedactMessages mutated the caller's MemoryCitations: %q", msgs[0].MemoryCitations[0].Note)
	}
	if !strings.Contains(msgs[1].Content, secret) {
		t.Fatalf("RedactMessages mutated the caller's Content: %q", msgs[1].Content)
	}
}
