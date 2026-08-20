package permission

import (
	"slices"
	"strings"
	"testing"
)

func TestValidateRule(t *testing.T) {
	known := func(id string) bool {
		switch id {
		case "bash", "edit_file", "write_file", "read_file", "grep":
			return true
		}
		return false
	}
	codes := func(rule string, known func(string) bool) []string {
		var out []string
		for _, p := range ValidateRule(rule, known) {
			out = append(out, p.Code)
		}
		return out
	}
	has := func(rule string, known func(string) bool, code string) bool {
		return slices.Contains(codes(rule, known), code)
	}

	tests := []struct {
		name       string
		rule       string
		known      func(string) bool
		wantErr    string // a code that must be error-severity, if any
		wantWarn   string // a code that must be warning-severity, if any
		wantNoCode string // a code that must be absent
	}{
		{name: "bare tool ok", rule: "Edit", wantNoCode: CodeUnknownTool},
		{name: "bash alias ok", rule: "Bash(go test:*)", known: known, wantNoCode: CodeUnknownTool},
		{name: "builtin id ok", rule: "edit_file", known: known, wantNoCode: CodeUnknownTool},
		{name: "empty rule", rule: "   ", wantErr: CodeInvalidRule},
		{name: "unparseable", rule: "()", wantErr: CodeInvalidRule},
		{name: "empty specifier", rule: "Edit()", wantErr: CodeEmptySpecifier},
		{name: "whitespace-only specifier", rule: "Edit( )", wantErr: CodeEmptySpecifier},
		{name: "empty literal", rule: "Bash=", wantErr: CodeEmptySpecifier},
		{name: "padded specifier warns", rule: "Edit( src/** )", wantWarn: CodePaddedSpecifier},
		{name: "padded legacy literal warns", rule: "Bash= git status", wantWarn: CodePaddedSpecifier},
		{name: "unpadded legacy literal ok", rule: "Bash=git status", wantNoCode: CodePaddedSpecifier},
		{name: "unbalanced paren", rule: "Edit(src", wantErr: CodeMalformedSpecifier},
		{name: "trailing junk", rule: "Edit(src))x", wantErr: CodeMalformedSpecifier},
		{name: "unknown tool warns", rule: "edit_fil(src/**)", known: known, wantWarn: CodeUnknownTool},
		{name: "no known skips unknown check", rule: "edit_fil(src/**)", wantNoCode: CodeUnknownTool},
		{name: "absolute path warns", rule: "Edit(/etc/passwd)", known: known, wantWarn: CodeOutsideWorkspace},
		{name: "windows absolute warns", rule: "Edit(C:\\tmp\\**)", known: known, wantWarn: CodeOutsideWorkspace},
		{name: "escape warns", rule: "Edit(../**)", known: known, wantWarn: CodeOutsideWorkspace},
		{name: "relative ok", rule: "Edit(src/**)", known: known, wantNoCode: CodeOutsideWorkspace},
		{name: "bash slash command no path warning", rule: "Bash(sed -i s/x/y/ /etc/passwd)", known: known, wantNoCode: CodeOutsideWorkspace},
		{name: "grep pattern no path warning", rule: "grep(/usr/bin)", known: known, wantNoCode: CodeOutsideWorkspace},
		{name: "glob pattern no path warning", rule: "glob(/etc/**)", known: known, wantNoCode: CodeOutsideWorkspace},
		{name: "literal legacy form ok", rule: "Bash=git status", known: known, wantNoCode: CodeEmptySpecifier},
		{name: "file_mutation group accepted", rule: "Edit", known: known, wantNoCode: CodeUnknownTool},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var errSev, warnSev bool
			for _, p := range ValidateRule(tc.rule, tc.known) {
				if p.Severity != "error" && p.Severity != "warning" {
					t.Errorf("problem %q has invalid severity %q", p.Code, p.Severity)
				}
				if p.Severity == "error" && p.Code == tc.wantErr {
					errSev = true
				}
				if p.Severity == "warning" && p.Code == tc.wantWarn {
					warnSev = true
				}
			}
			if tc.wantErr != "" && !errSev {
				t.Errorf("rule %q: expected error-severity %s, got %v", tc.rule, tc.wantErr, codes(tc.rule, tc.known))
			}
			if tc.wantWarn != "" && !warnSev {
				t.Errorf("rule %q: expected warning-severity %s, got %v", tc.rule, tc.wantWarn, codes(tc.rule, tc.known))
			}
			if tc.wantNoCode != "" && has(tc.rule, tc.known, tc.wantNoCode) {
				t.Errorf("rule %q: did not expect %s, got %v", tc.rule, tc.wantNoCode, codes(tc.rule, tc.known))
			}
		})
	}
}

func TestValidateRuleMessagesAreConcrete(t *testing.T) {
	for _, p := range ValidateRule("Edit(src", func(string) bool { return true }) {
		if strings.TrimSpace(p.Message) == "" {
			t.Error("problem has empty message")
		}
	}
}
