package builtin

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/base/secrets"
)

// A secret the agent was given reaches its commands as a variable, read on
// each command; whatever a command prints of it comes back masked.
func TestABashCommandReadsAGivenSecretAndPrintsItMasked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the command below is POSIX shell")
	}
	secrets.RegisterValue("SHOP_KEY", "sk-shop-0123456789")
	defer secrets.UnregisterValue("sk-shop-0123456789")
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
