package cli

import (
	"strings"
	"testing"

	"reasonix/internal/contract/config"
)

func TestResolveModelForCLIMissingKeyNamesCredentialFile(t *testing.T) {
	isolateCLIConfigHome(t)
	const keyEnv = "REASONIX_CLI_TEST_SHELL_ONLY_KEY"
	t.Setenv(keyEnv, "sk-from-shell")
	cfg := &config.Config{Providers: []config.ProviderEntry{
		{Name: "deepseek-flash", Kind: "openai", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash", APIKeyEnv: keyEnv},
	}}

	_, _, err := resolveModelForCLI("deepseek-flash", cfg)
	if err == nil {
		t.Fatal("expected an error for a model whose key is only in the shell environment")
	}
	msg := err.Error()
	for _, want := range []string{`model "deepseek-flash"`, keyEnv + "=<key>", cfg.Roots().UserCredentialsPath(), "reasonix setup"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not mention %q", msg, want)
		}
	}
}
