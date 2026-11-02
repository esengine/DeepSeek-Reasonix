package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/base/secrets/secretstest"
	"reasonix/internal/contract/tool"
)

// A secret the agent was given reaches its commands as a variable, read on
// each command; whatever a command prints of it comes back masked.
func TestABashCommandReadsAGivenSecretAndPrintsItMasked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the command below is POSIX shell")
	}
	secretstest.Hold("SHOP_KEY", "sk-shop-0123456789")
	given := []string{"REASONIX_SECRET_SHOP_KEY=sk-shop-0123456789"}
	b := bash{workDir: t.TempDir(), secretEnv: func() []string { return given }}
	args, _ := json.Marshal(map[string]string{"command": `printf 'key=%s set=%s' "$REASONIX_SECRET_SHOP_KEY" "${REASONIX_SECRET_SHOP_KEY:+yes}"`})
	out, err := b.Execute(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "sk-shop") || !strings.Contains(out, "key=[secret:SHOP_KEY]") || !strings.Contains(out, "set=yes") {
		t.Fatalf("output = %q", out)
	}
	given = nil
	out, _ = b.Execute(context.Background(), args)
	if !strings.Contains(out, "set=") || strings.Contains(out, "set=yes") {
		t.Fatalf("a secret no longer given still reached the command: %q", out)
	}
}

// A failing command's output tail is kept with the transcript and drawn in the
// window, and what it streams while it runs is drawn too: each is masked.
func TestAFailingCommandsTailAndProgressAreMasked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the command below is POSIX shell")
	}
	secretstest.Hold("TAIL_KEY", "sk-tail-0123456789")
	given := []string{"REASONIX_SECRET_TAIL_KEY=sk-tail-0123456789"}
	b := bash{workDir: t.TempDir(), secretEnv: func() []string { return given }}
	var streamed strings.Builder
	ctx := tool.WithProgress(context.Background(), func(chunk string) { streamed.WriteString(chunk) })
	args, _ := json.Marshal(map[string]string{"command": `echo "trace $REASONIX_SECRET_TAIL_KEY"; exit 3`})
	res, _ := b.ExecuteDetailed(ctx, args)
	if res.Execution == nil || !strings.Contains(res.Execution.OutputTail, "trace [secret:TAIL_KEY]") || strings.Contains(res.Execution.OutputTail, "sk-tail") {
		t.Fatalf("tail = %+v", res.Execution)
	}
	if strings.Contains(streamed.String(), "sk-tail") {
		t.Fatalf("progress = %q", streamed.String())
	}
}

// A model that read a masked copy writes the mark back; on disk that would
// replace the real value, so the write is refused and says why.
func TestAWriteCarryingASecretsMarkIsRefused(t *testing.T) {
	secretstest.Hold("ENV_KEY", "sk-env-0123456789")
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("KEY=sk-env-0123456789\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, run := range map[string]func() error{
		"write_file": func() error {
			args, _ := json.Marshal(map[string]string{"path": path, "content": "KEY=[secret:ENV_KEY]\nMORE=1\n"})
			_, err := writeFile{workDir: dir, roots: realRoots([]string{dir})}.Execute(context.Background(), args)
			return err
		},
		"edit_file": func() error {
			args, _ := json.Marshal(map[string]string{"path": path, "old_string": "KEY=[secret:ENV_KEY]", "new_string": "KEY=[secret:ENV_KEY]\nMORE=1"})
			_, err := editFile{}.Execute(context.Background(), args)
			return err
		},
		"write_file preview": func() error {
			args, _ := json.Marshal(map[string]string{"path": path, "content": "KEY=[secret:ENV_KEY]\n"})
			_, err := writeFile{workDir: dir, roots: realRoots([]string{dir})}.Preview(context.Background(), args)
			return err
		},
	} {
		var refusal tool.Refusal
		if err := run(); !errors.As(err, &refusal) || refusal.Code != CodeMaskedSecretWrite || !strings.Contains(refusal.Message, "$REASONIX_SECRET_ENV_KEY") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if data, _ := os.ReadFile(path); string(data) != "KEY=sk-env-0123456789\n" {
		t.Fatalf("the file changed: %q", data)
	}
}

// A command carrying a mark would act on the mark, not the value: it is
// refused before anything runs, and told how to read the value instead.
func TestAShellCommandCarryingASecretsMarkIsRefused(t *testing.T) {
	secretstest.Hold("CMD_KEY", "sk-command-0123456789")
	dir := t.TempDir()
	b := bash{workDir: dir}
	args, _ := json.Marshal(map[string]string{"command": "echo 'KEY=[secret:CMD_KEY]' > " + filepath.Join(dir, "out.env")})
	_, err := b.Execute(context.Background(), args)
	var refusal tool.Refusal
	if !errors.As(err, &refusal) || refusal.Code != CodeMaskedSecretCommand || !strings.Contains(refusal.Message, "$REASONIX_SECRET_CMD_KEY") {
		t.Fatalf("err = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "out.env")); statErr == nil {
		t.Fatal("the command ran")
	}
}

func TestUnrelatedShellCommandsReceiveNoGrantedSecrets(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell")
	}
	b := bash{workDir: t.TempDir(), secretEnv: func() []string { return []string{"REASONIX_SECRET_SHOP_KEY=private-123456789"} }}
	for _, command := range []string{`env`, `name=REASONIX_SECRET_SHOP_KEY; printenv "$name"`, `printf '%s' '$REASONIX_SECRET_SHOP_KEY'`} {
		args, _ := json.Marshal(map[string]string{"command": command})
		out, _ := b.Execute(t.Context(), args)
		if strings.Contains(out, "private-123456789") {
			t.Fatalf("unreferenced secret reached %q", command)
		}
	}
}
