// Package sanitize provides output sanitization for tool results and session
// data, preventing accidental credential exposure in the LLM context and on
// disk. It is the core defense layer against passive API-key leaks (see #6178).
package sanitize

import (
	"regexp"
	"strings"
)

// Common credential patterns that must be redacted from tool outputs, session
// logs, and any text that enters the LLM context or is persisted to disk.
//
// The redaction strategy follows Claude Code's approach: preserve the first 4
// and last 4 characters of the secret, replacing the middle with asterisks, so
// the user can still identify _which_ key was exposed without the full secret
// leaving the runtime.
var (
	// apiKeyPattern matches common API key formats:
	//   sk-<alphanumeric>  (OpenAI, DeepSeek, etc.)
	//   pk-<alphanumeric>  (public keys)
	reAPIKey = regexp.MustCompile(`(?i)\b((?:sk|pk|fk)[A-Za-z0-9_-]{12,})\b`)

	// gitHubToken matches GitHub personal access tokens:
	//   ghp_***, gho_***, ghu_***, ghs_***, ghr_***
	reGitHubToken = regexp.MustCompile(`\b(gh[opusr]_[A-Za-z0-9]{12,})\b`)

	// slackToken matches Slack tokens:
	//   xoxb-*** (bots), xoxa-*** (apps), xoxp-*** (user), xoxr-*** (restricted), xoxs-*** (system)
	reSlackToken = regexp.MustCompile(`\b(xox[baprs]-[A-Za-z0-9-]{6,})\b`)

	// awsKey matches AWS access key IDs:
	//   AKIA<alphanumeric> (standard), ASIA<alphanumeric> (temporary)
	reAWSKey = regexp.MustCompile(`\b((?:AKIA|ASIA)[A-Z0-9]{16})\b`)

	// envSecretLine matches lines that assign a value to a secret-suffixed environment
	// variable, e.g.:
	//   DEEPSEEK_API_KEY=sk-xxxx
	//   OPENAI_API_KEY=sk-xxxx
	//   AWS_SECRET_ACCESS_KEY=xxxx
	//   GITHUB_TOKEN=ghp_xxxx
	//   ANYTHING_SECRET=xxxx
	//   ANYTHING_TOKEN=xxxx
	//   ANYTHING_PASSWORD=xxxx
	// This catches inline values that the key-specific patterns above might miss
	// because they lack the signature prefix (e.g. a raw base64 secret).
	//
	// We match _KEY=, _SECRET=, _TOKEN=, _PASSWORD= (case-insensitive left side)
	// and redact the value portion.
	reEnvSecretLine = regexp.MustCompile(`(?m)^\s*([A-Za-z_][A-Za-z0-9_]*?(?:API_KEY|SECRET|TOKEN|PASSWORD|PASS|CREDENTIALS|SIGNING_KEY|ACCESS_KEY))\s*[=:]\s*(\S+)\s*$`)

	// genericSecretEnvLine matches any env var whose value looks like a credential
	// even without a known suffix, e.g. PRIVATE_KEY=-----BEGIN..., DB_URL=postgres://user:pass@...
	// This is a broader but fuzzier catch-all.
	reGenericSecretValue = regexp.MustCompile(`(?m)^\s*([A-Za-z_][A-Za-z0-9_]*?)\s*[=:]\s*(-----BEGIN[A-Za-z0-9+/=\s-]+?-----END)`)
)

// RedactCredentials replaces credential values in text with a redacted form
// that preserves only the first 4 and last 4 characters.
//
// It handles:
//   - API key tokens (sk-*, pk-*, fk-*)
//   - GitHub tokens (ghp_*, gho_*, etc.)
//   - Slack tokens (xox*-*)
//   - AWS access keys (AKIA*, ASIA*)
//   - Environment variable lines matching *_KEY=*, *_SECRET=*, *_TOKEN=*, *_PASSWORD=*
//   - PEM-encoded private keys
//
// The function is safe to call on arbitrary text — it only modifies known
// credential patterns and leaves everything else unchanged.
func RedactCredentials(text string) string {
	if text == "" {
		return ""
	}

	// Phase 1: Redact inline credential tokens (API keys, GitHub tokens, etc.)
	text = reAPIKey.ReplaceAllStringFunc(text, redactMiddle)
	text = reGitHubToken.ReplaceAllStringFunc(text, redactMiddle)
	text = reSlackToken.ReplaceAllStringFunc(text, redactMiddle)
	text = reAWSKey.ReplaceAllStringFunc(text, redactMiddle)

	// Phase 2: Redact env-var lines with known secret suffixes
	text = reEnvSecretLine.ReplaceAllStringFunc(text, redactEnvValue)

	// Phase 3: Redact generic secret values (PEM blocks, etc.)
	text = reGenericSecretValue.ReplaceAllStringFunc(text, redactGenericValue)

	return text
}

// redactMiddle replaces the middle portion of a credential with asterisks,
// preserving the first 4 and last 4 characters. If the value is shorter than
// 12 chars it redacts all but first 2 and last 2.
func redactMiddle(val string) string {
	if len(val) <= 8 {
		// Too short to preserve meaningfully; redact fully except prefix
		prefixEnd := strings.IndexAny(val, "_ -")
		if prefixEnd < 0 || prefixEnd >= len(val)-1 {
			return val[:2] + "****" + val[len(val)-2:]
		}
		prefix := val[:prefixEnd+1]
		rest := val[prefixEnd+1:]
		if len(rest) <= 4 {
			return prefix + "****"
		}
		return prefix + rest[:2] + "****" + rest[len(rest)-2:]
	}
	if len(val) <= 12 {
		return val[:2] + strings.Repeat("*", len(val)-4) + val[len(val)-2:]
	}
	return val[:4] + strings.Repeat("*", len(val)-8) + val[len(val)-4:]
}

// redactEnvValue redacts the value portion of a "KEY=VALUE" or "KEY: VALUE" line.
func redactEnvValue(line string) string {
	idx := strings.IndexAny(line, "=:")
	if idx < 0 {
		return line
	}
	key := line[:idx+1]
	value := strings.TrimSpace(line[idx+1:])

	// Strip surrounding quotes
	value = strings.Trim(value, `"'`)

	if value == "" {
		return line
	}

	redacted := redactMiddle(value)
	if strings.Contains(line, "'") {
		return key + "'" + redacted + "'"
	} else if strings.Contains(line, `"`) {
		return key + `"` + redacted + `"`
	}
	return key + redacted
}

// redactGenericValue redacts the value after the "=" sign for multi-line values
// like PEM blocks.
func redactGenericValue(line string) string {
	idx := strings.IndexAny(line, "=:")
	if idx < 0 {
		return line
	}
	key := line[:idx+1]
	value := strings.TrimSpace(line[idx+1:])
	redacted := redactMiddle(value)
	return key + redacted
}
