package mcpsetup

import (
	"strings"
	"testing"
)

func TestNameFromArgvNeverDerivesFromMaskedValue(t *testing.T) {
	for _, args := range [][]string{
		{"-y", "--api-key", "sk-live-0123456789", "pkg-server"},
		{"-y", "--token=tok-0123456789", "pkg-server"},
		{"-y", "-e", "API_KEY=sk-live-0123456789", "pkg-server"},
	} {
		if got := NameFromArgv("npx", args); strings.Contains(got, "0123456789") {
			t.Errorf("name for %v = %q carries a masked value", args, got)
		}
	}
}

func TestNameFromArgvOperandIsTheShownArgument(t *testing.T) {
	for _, c := range []struct {
		command string
		args    []string
	}{
		{"npx", []string{"--token", "s3cretvalue", "--name", "xs3cretvaluex"}},
		{"npx", []string{"--token", "s3cretvalue", "s3cretvalue-extra"}},
		{"uvx", []string{"--api-key", "s3cretvalue", "--from", "s3cretvalue2"}},
	} {
		if got := NameFromArgv(c.command, c.args); strings.Contains(got, "s3cretvalue") {
			t.Errorf("name for %v = %q carries a masked value", c.args, got)
		}
	}
}
