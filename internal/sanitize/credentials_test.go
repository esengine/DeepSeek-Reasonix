package sanitize

import (
	"strings"
	"testing"
)

func TestRedactCredentials_APIKey(t *testing.T) {
	tests := []struct {
		name  string
		input string
		leak  string // substring that must NOT appear in output
	}{
		{
			name:  "OpenAI key inline",
			input: `api_key = "sk-proj-XyZ123AbC456DeF789GhIjKlMnOpQrStUvWxYz0123456789abcdef"`,
			leak:  `sk-proj-XyZ123AbC456DeF789GhIjKlMnOpQrStUvWxYz0123456789abcdef`,
		},
		{
			name:  "DeepSeek key in env file",
			input: `DEEPSEEK_API_KEY=sk-abcdef1234567890abcdef1234567890abcdef12`,
			leak:  `sk-abcdef1234567890abcdef1234567890abcdef12`,
		},
		{
			name:  "short key",
			input: `KEY=sk-abcdef123456`,
			leak:  `sk-abcdef123456`,
		},
		{
			name:  "no match normal text",
			input: `This is a normal sentence with no keys.`,
			leak:  ``,
		},
		{
			name:  "multiple keys in one output",
			input: "API Key 1: sk-aaaa1111bbbb2222cccc3333dddd4444\nAPI Key 2: sk-eeee5555ffff6666gggg7777hhhh8888",
			leak:  `sk-aaaa1111bbbb2222cccc3333dddd4444`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactCredentials(tt.input)
			if tt.leak == "" {
				if got != tt.input {
					t.Errorf("changed normal text:\n  input: %q\n   got: %q", tt.input, got)
				}
				return
			}
			if strings.Contains(got, tt.leak) {
				t.Errorf("leaked original secret %q in output: %q", tt.leak, got)
			}
			if !strings.Contains(got, "****") {
				t.Errorf("did not add asterisks: %q", got)
			}
			t.Logf("Redacted: %q", got)
		})
	}
}

func TestRedactCredentials_GitHubToken(t *testing.T) {
	input := `token=ghp_TESTTESTTESTtesttoken1234567890TESTTOKEN`
	got := RedactCredentials(input)
	if strings.Contains(got, "ghp_TESTTESTTEST") {
		t.Errorf("GitHub token was not redacted: %q", got)
	}
	if !strings.Contains(got, "****") {
		t.Errorf("Redacted output should contain asterisks: %q", got)
	}
}

func TestRedactCredentials_SlackToken(t *testing.T) {
	input := `SLACK_BOT_TOKEN=xoxb-FAKE12345678-FAKE12345678-fakeTokenFakeToken`
	got := RedactCredentials(input)
	if strings.Contains(got, "xoxb-FAKE12345678") {
		t.Errorf("Slack token was not redacted: %q", got)
	}
}

func TestRedactCredentials_AWSKey(t *testing.T) {
	input := `AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE`
	got := RedactCredentials(input)
	if strings.Contains(got, "AKIAIOSFODNN7EXAMPLE") {
		t.Errorf("AWS key was not redacted: %q", got)
	}
	if !strings.Contains(got, "****") {
		t.Errorf("AWS key output missing asterisks: %q", got)
	}
}

func TestRedactCredentials_EnvSecretLine(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"API_KEY suffix", `MYAPP_API_KEY=my-super-secret-value-12345`},
		{"SECRET suffix", `DB_PASSWORD_SECRET=sup3rS3cr3t!`},
		{"TOKEN suffix", `GITHUB_TOKEN=ghp_TESTTESTTESTtesttoken1234567890TESTTOKEN`},
		{"PASSWORD suffix", `REDIS_PASSWORD=correct-horse-battery-staple`},
		{"CREDENTIALS suffix", `AWS_CREDENTIALS=AKID+secretKeyHere`},
		{"ACCESS_KEY suffix", `AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactCredentials(tt.input)
			if got == tt.input {
				t.Errorf("did not redact %q: got %q", tt.name, got)
			}
			if !strings.Contains(got, "****") {
				t.Errorf("missing asterisks: %q", got)
			}
			t.Logf("Redacted: %q", got)
		})
	}
}

func TestRedactCredentials_PEMKey(t *testing.T) {
	input := `PRIVATE_KEY=-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEA0OcVFhFpVnFqFhFpVnFqFhFpVnFq
-----END RSA PRIVATE KEY-----`
	got := RedactCredentials(input)
	if strings.Contains(got, "BEGIN RSA PRIVATE KEY") && !strings.Contains(got, "****") {
		t.Errorf("PEM key was not redacted: %q", got)
	}
}

func TestRedactCredentials_NormalTextUnchanged(t *testing.T) {
	inputs := []string{
		"Hello, world!",
		"package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"hi\") }",
		"The answer is 42.",
		"git commit -m \"fix: resolve issue #1234\"",
		"SELECT * FROM users WHERE id = 1;",
		`{"role": "user", "content": "What is the weather?"}`,
		"/Users/user/project/main.go:42: some content here",
		"Task list:\n- [x] Done\n- [ ] Pending",
	}

	for _, input := range inputs {
		t.Run(input[:min(30, len(input))], func(t *testing.T) {
			got := RedactCredentials(input)
			if got != input {
				t.Errorf("changed normal text:\n  input: %q\n   got: %q", input, got)
			}
		})
	}
}

func TestRedactCredentials_SessionLogContent(t *testing.T) {
	input := `{"role":"assistant","content":"I found your API keys:\nDEEPSEEK_API_KEY=sk-abcdef1234567890abcdef1234567890abcdef12\nOPENAI_API_KEY=sk-proj-xyZ789AbC456DeF789GhIjKlMnOpQrStUvWxYz0123456789\n"}`
	got := RedactCredentials(input)
	if strings.Contains(got, "sk-abcdef1234567890") {
		t.Errorf("Session log content was not redacted: %q", got)
	}
	if !strings.Contains(got, "****") {
		t.Errorf("Redacted session log should contain asterisks: %q", got)
	}
	t.Logf("Redacted session line: %s", got)
}

func TestRedactCredentials_BashEnvOutput(t *testing.T) {
	input := `SHELL=/bin/zsh
HOME=/Users/user
PATH=/usr/local/bin:/usr/bin
DEEPSEEK_API_KEY=sk-abcdef1234567890abcdef1234567890abcdef12
OPENAI_API_KEY=sk-proj-XyZ123AbC456DeF789GhIjKlMnOpQrStUvWxYz0123456789abcdef
AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
GITHUB_TOKEN=ghp_TESTTESTTESTtesttoken1234567890TESTTOKEN
LANG=en_US.UTF-8`
	got := RedactCredentials(input)
	if strings.Contains(got, "sk-abcdef1234567890") {
		t.Errorf("printenv output was not redacted")
	}
	if strings.Contains(got, "wJalrXUtnFEMI") {
		t.Errorf("AWS secret key in env output was not redacted")
	}
	if strings.Contains(got, "ghp_TESTTESTTESTtesttoken") {
		t.Errorf("GitHub token in env output was not redacted")
	}
	if !strings.Contains(got, "SHELL=/bin/zsh") {
		t.Errorf("Normal env var SHELL was incorrectly modified")
	}
	if !strings.Contains(got, "HOME=/Users/user") {
		t.Errorf("Normal env var HOME was incorrectly modified")
	}
	if !strings.Contains(got, "LANG=en_US.UTF-8") {
		t.Errorf("Normal env var LANG was incorrectly modified")
	}
	t.Logf("Redacted env output:\n%s", got)
}
