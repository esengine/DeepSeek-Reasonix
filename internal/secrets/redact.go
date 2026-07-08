package secrets

import (
	"os"
	"regexp"
	"strings"
	"sync/atomic"

	"reasonix/internal/provider"
)

var (
	// secretKeyNamePattern matches environment-variable / key names that are
	// likely to carry credentials. Bare "pwd" is intentionally excluded: it
	// only counts with a leading separator (DB_PWD, MYSQL-PWD), so the POSIX
	// PWD / OLDPWD working-directory variables never match.
	secretKeyNamePattern = regexp.MustCompile(`(?i)((^|[_-])(api[_-]?key|access[_-]?key|private[_-]?key|secret|token|password|passwd)([_-]|$)|[_-]pwd([_-]|$))`)
	// keyValuePattern mirrors secretKeyNamePattern for KEY=value / key: value
	// text: PWD requires a prefixed separator so "PWD=/home/user" stays intact.
	// The optional auth-scheme group keeps schemes like "Basic"/"Digest" out of
	// the value capture, so the credential after the scheme word is what gets
	// masked (an uncaptured scheme would itself be swallowed as the value,
	// leaving the real credential in the clear right behind it).
	keyValuePattern     = regexp.MustCompile(`(?i)\b([A-Z0-9_.-]*(?:API[_-]?KEY|ACCESS[_-]?KEY|PRIVATE[_-]?KEY|SECRET|TOKEN|PASSWORD|PASSWD)[A-Z0-9_.-]*|[A-Z0-9_.-]+[_-]PWD[A-Z0-9_.-]*|AUTHORIZATION)\b(\s*[:=]\s*)((?:Bearer|Basic|Digest|Negotiate|NTLM|Token|Bot|ApiKey)\s+)?(['"]?)([^'"\s,;]+)(['"]?)`)
	bearerTokenPattern  = regexp.MustCompile(`(?i)\bBearer\s+([A-Za-z0-9._~+/=-]{16,})`)
	openAIKeyPattern    = regexp.MustCompile(`\b((?:sk|rk)-(?:proj-)?[A-Za-z0-9_-]{12,})\b`)
	githubTokenPattern  = regexp.MustCompile(`\b(gh[pousr]_[A-Za-z0-9_]{20,}|github_pat_[A-Za-z0-9_]{20,})\b`)
	slackTokenPattern   = regexp.MustCompile(`\b(xox[baprs]-[A-Za-z0-9-]{16,})\b`)
	awsAccessKeyPattern = regexp.MustCompile(`\b(AKIA[0-9A-Z]{16}|ASIA[0-9A-Z]{16})\b`)
	jwtPattern          = regexp.MustCompile(`\b(eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+)\b`)
)

const redactedValue = "[redacted]"

// Runtime toggles for the opt-in protection layers, set once by the
// composition root from the user-global [secrets] config section. Package
// globals are safe here because [secrets] cannot be overridden per-project:
// every concurrent workspace in one process shares the same user setting.
var (
	redactToolOutputEnabled      atomic.Bool
	filterSubprocessEnvEnabled   atomic.Bool
	protectSensitiveFilesEnabled atomic.Bool
)

func init() {
	// Tool-output redaction defaults on; subprocess env filtering defaults
	// off because it breaks legitimate token-based workflows (gh, git push
	// over HTTPS, npm publish) and needs an explicit user opt-in.
	redactToolOutputEnabled.Store(true)
}

// SetRedactToolOutput enables or disables masking of tool output before it
// enters model context and UI events ([secrets] redact_tool_output). Durable
// surfaces — session transcripts and background-job artifacts — are always
// redacted regardless of this toggle.
func SetRedactToolOutput(enabled bool) { redactToolOutputEnabled.Store(enabled) }

// SetFilterSubprocessEnv enables or disables stripping credential-like
// variables from tool subprocess environments ([secrets]
// filter_subprocess_env).
func SetFilterSubprocessEnv(enabled bool) { filterSubprocessEnvEnabled.Store(enabled) }

// SetProtectSensitiveFiles enables or disables the built-in credential-path
// read denylist for read/list/search tools ([secrets] protect_sensitive_files).
func SetProtectSensitiveFiles(enabled bool) { protectSensitiveFilesEnabled.Store(enabled) }

// ProtectSensitiveFiles reports whether the built-in credential-path read
// denylist is active.
func ProtectSensitiveFiles() bool { return protectSensitiveFilesEnabled.Load() }

// EnvKeySensitive reports whether an environment variable name is likely to
// carry credentials. It intentionally keys off the name, not the value, so child
// processes do not inherit saved provider secrets when filtering is enabled.
func EnvKeySensitive(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	return secretKeyNamePattern.MatchString(key)
}

// FilterEnv removes sensitive KEY=value assignments from an environment vector.
func FilterEnv(env []string) []string {
	out := env[:0]
	for _, item := range env {
		key, _, ok := strings.Cut(item, "=")
		if !ok || EnvKeySensitive(key) {
			continue
		}
		out = append(out, item)
	}
	return out
}

// ProcessEnv returns the environment for shell/tool subprocesses: the current
// process environment as-is by default, with credential-like assignments
// removed when the user opted into [secrets] filter_subprocess_env.
func ProcessEnv() []string {
	if !filterSubprocessEnvEnabled.Load() {
		return os.Environ()
	}
	return FilterEnv(os.Environ())
}

// RedactToolOutput masks credential-like values in live tool output (model
// context, UI events) unless the user disabled [secrets] redact_tool_output.
// Durable writers (session save, job artifacts) call Redact directly instead:
// disk logs stay redacted even when live output is not.
func RedactToolOutput(s string) string {
	if !redactToolOutputEnabled.Load() {
		return s
	}
	return Redact(s)
}

// Redact masks credential-like values in text before the text enters durable
// transcripts, job artifacts, or diagnostic records. It is deterministic and
// idempotent: redacting already-redacted text is a byte-for-byte no-op, which
// the session save path relies on for digest stability across load/save cycles
// (see Session.save).
func Redact(s string) string {
	if s == "" {
		return s
	}
	s = keyValuePattern.ReplaceAllStringFunc(s, func(match string) string {
		parts := keyValuePattern.FindStringSubmatch(match)
		if len(parts) != 7 {
			return redactedValue
		}
		key := parts[1]
		sep := parts[2]
		scheme := parts[3]
		quote := parts[4]
		value := parts[5]
		endQuote := parts[6]
		if strings.EqualFold(key, "authorization") {
			return key + sep + scheme + quote + redactedValue + endQuote
		}
		return key + sep + scheme + quote + mask(value) + endQuote
	})
	s = bearerTokenPattern.ReplaceAllStringFunc(s, func(match string) string {
		token := strings.TrimSpace(strings.TrimPrefix(match, "Bearer"))
		if len(token) == len(match) {
			return "Bearer " + redactedValue
		}
		return "Bearer " + mask(token)
	})
	for _, rx := range []*regexp.Regexp{openAIKeyPattern, githubTokenPattern, slackTokenPattern, awsAccessKeyPattern, jwtPattern} {
		s = rx.ReplaceAllStringFunc(s, mask)
	}
	return s
}

func mask(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return redactedValue
	}
	if len(value) <= 12 {
		return redactedValue
	}
	head := 4
	tail := 4
	if strings.HasPrefix(value, "sk-") || strings.HasPrefix(value, "rk-") {
		head = 6
	}
	if len(value) <= head+tail {
		return redactedValue
	}
	return value[:head] + strings.Repeat("*", len(value)-head-tail) + value[len(value)-tail:]
}

// RedactMessage returns a storage-safe copy of m with textual secret surfaces
// masked. Images are left untouched because they are opaque data URLs.
// ToolCalls and MemoryCitations are cloned before masking: m is passed by
// value but its slices share backing arrays with the caller, and the save
// path hands in live session messages — writing through would silently mutate
// the model-visible history mid-conversation and churn the prompt cache.
func RedactMessage(m provider.Message) provider.Message {
	m.Content = Redact(m.Content)
	m.ReasoningContent = Redact(m.ReasoningContent)
	m.Original = Redact(m.Original)
	if len(m.ToolCalls) > 0 {
		calls := make([]provider.ToolCall, len(m.ToolCalls))
		copy(calls, m.ToolCalls)
		for i := range calls {
			calls[i].Arguments = Redact(calls[i].Arguments)
			calls[i].Diff = Redact(calls[i].Diff)
		}
		m.ToolCalls = calls
	}
	if len(m.MemoryCitations) > 0 {
		cites := make([]provider.MemoryCitation, len(m.MemoryCitations))
		copy(cites, m.MemoryCitations)
		for i := range cites {
			cites[i].Note = Redact(cites[i].Note)
		}
		m.MemoryCitations = cites
	}
	return m
}

// RedactMessages returns a redacted copy of msgs. The input slice and its
// messages are never mutated.
func RedactMessages(msgs []provider.Message) []provider.Message {
	out := make([]provider.Message, len(msgs))
	for i, m := range msgs {
		out[i] = RedactMessage(m)
	}
	return out
}
