package secrets

import (
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"

	"reasonix/internal/provider"
)

var (
	// secretKeyNamePattern matches environment-variable / key names that are
	// likely to carry credentials. Bare "pwd" is intentionally excluded: it
	// only counts with a leading separator (DB_PWD, MYSQL-PWD), so the POSIX
	// PWD / OLDPWD working-directory variables never match.
	secretKeyNamePattern = regexp.MustCompile(`(?i)((^|[_-])(api[_-]?key|access[_-]?key|private[_-]?key|secret|token|password|passwd)([_-]|$)|[_-]pwd([_-]|$))`)
	// cookieHeaderPattern captures Cookie/Set-Cookie header values so every
	// name=value pair gets its value masked; attribute flags without a value
	// (HttpOnly, Secure) pass through untouched.
	cookieHeaderPattern = regexp.MustCompile(`(?i)\b((?:set-)?cookie)(\s*[:=]\s*)([^=;\s]+=[^;\s]*(?:;\s*[^=;\s]+(?:=[^;\s]*)?)*)`)
	cookiePairPattern   = regexp.MustCompile(`([^=;\s]+)=([^;\s]*)`)
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
	filterSubprocessEnvEnabled   atomic.Bool
	protectSensitiveFilesEnabled atomic.Bool
	credentialEnvKeys            = struct {
		sync.RWMutex
		keys map[string]struct{}
	}{keys: map[string]struct{}{}}
)

// SetFilterSubprocessEnv enables or disables stripping credential-like
// variables from tool subprocess environments ([secrets]
// filter_subprocess_env).
func SetFilterSubprocessEnv(enabled bool) { filterSubprocessEnvEnabled.Store(enabled) }

// FilterSubprocessEnv reports whether credential-like variables are stripped
// from tool subprocess environments. Callers that would launch a command in an
// environment they cannot filter (a host-owned terminal, say) must check this
// and keep execution local.
func FilterSubprocessEnv() bool { return filterSubprocessEnvEnabled.Load() }

// SetProtectSensitiveFiles enables or disables the built-in credential-path
// read denylist for read/list/search tools ([secrets] protect_sensitive_files).
func SetProtectSensitiveFiles(enabled bool) { protectSensitiveFilesEnabled.Store(enabled) }

// ProtectSensitiveFiles reports whether the built-in credential-path read
// denylist is active.
func ProtectSensitiveFiles() bool { return protectSensitiveFilesEnabled.Load() }

// RegisterCredentialEnvKeys permanently marks names whose values came from
// Reasonix's credential store. Registration is a process-lifetime union so two
// concurrent workspaces with different custom providers cannot make each
// other's saved keys visible to tools. Explicit per-tool/plugin env config may
// still add a value back after ProcessEnv has produced the safe base env.
func RegisterCredentialEnvKeys(keys []string) {
	credentialEnvKeys.Lock()
	defer credentialEnvKeys.Unlock()
	for _, key := range keys {
		if key = credentialEnvKey(key); key != "" {
			credentialEnvKeys.keys[key] = struct{}{}
		}
	}
}

func credentialEnvKey(key string) string {
	return strings.ToUpper(strings.TrimSpace(key))
}

func registeredCredentialEnvKey(key string) bool {
	credentialEnvKeys.RLock()
	defer credentialEnvKeys.RUnlock()
	_, ok := credentialEnvKeys.keys[credentialEnvKey(key)]
	return ok
}

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
		if !ok || EnvKeySensitive(key) || registeredCredentialEnvKey(key) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func filterRegisteredCredentialEnv(env []string) []string {
	out := env[:0]
	for _, item := range env {
		key, _, ok := strings.Cut(item, "=")
		if !ok || registeredCredentialEnvKey(key) {
			continue
		}
		out = append(out, item)
	}
	return out
}

// ProcessEnv returns the environment for shell/tool subprocesses. Values loaded
// from Reasonix's credential store are always removed. Other credential-like
// inherited variables are removed only when the user opted into [secrets]
// filter_subprocess_env, preserving existing gh/git/npm workflows by default.
func ProcessEnv() []string {
	if !filterSubprocessEnvEnabled.Load() {
		return filterRegisteredCredentialEnv(os.Environ())
	}
	return FilterEnv(os.Environ())
}

// Redact masks credential-like values for explicit diagnostic, export, and
// cleanup paths. Normal model content, tool output, session transcripts, and
// background-job artifacts deliberately bypass this helper to retain v0.53's
// byte-preserving behavior.
func Redact(s string) string {
	if s == "" {
		return s
	}
	s = redactKeyValues(s)
	s = cookieHeaderPattern.ReplaceAllStringFunc(s, func(match string) string {
		parts := cookieHeaderPattern.FindStringSubmatch(match)
		if len(parts) != 4 {
			return redactedValue
		}
		return parts[1] + parts[2] + cookiePairPattern.ReplaceAllString(parts[3], "$1="+redactedValue)
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

func redactKeyValues(s string) string {
	var out strings.Builder
	last := 0
	for sep := 0; sep < len(s); sep++ {
		if s[sep] != ':' && s[sep] != '=' {
			continue
		}
		keyEnd := sep
		for keyEnd > 0 && asciiSpace(s[keyEnd-1]) {
			keyEnd--
		}
		if keyEnd > 0 && (s[keyEnd-1] == '\'' || s[keyEnd-1] == '"') {
			keyEnd--
		}
		keyStart := keyEnd
		for keyStart > 0 && credentialKeyByte(s[keyStart-1]) {
			keyStart--
		}
		key := s[keyStart:keyEnd]
		if !credentialTextKeySensitive(key) {
			continue
		}

		valueStart := sep + 1
		for valueStart < len(s) && asciiSpace(s[valueStart]) {
			valueStart++
		}
		if valueStart < len(s) && (s[valueStart] == '\'' || s[valueStart] == '"') {
			valueStart++
		}
		schemeStart := valueStart
		for valueStart < len(s) && credentialKeyByte(s[valueStart]) {
			valueStart++
		}
		if valueStart < len(s) && asciiSpace(s[valueStart]) && authorizationScheme(s[schemeStart:valueStart]) {
			for valueStart < len(s) && asciiSpace(s[valueStart]) {
				valueStart++
			}
			if valueStart < len(s) && (s[valueStart] == '\'' || s[valueStart] == '"') {
				valueStart++
			}
		} else {
			valueStart = schemeStart
		}

		valueEnd := valueStart
		for valueEnd < len(s) && !asciiSpace(s[valueEnd]) && s[valueEnd] != '\'' && s[valueEnd] != '"' && s[valueEnd] != ',' && s[valueEnd] != ';' {
			valueEnd++
		}
		if valueEnd == valueStart {
			continue
		}
		if last == 0 {
			out.Grow(len(s))
		}
		out.WriteString(s[last:valueStart])
		if authorizationKey(key) {
			out.WriteString(redactedValue)
		} else {
			out.WriteString(mask(s[valueStart:valueEnd]))
		}
		last = valueEnd
		sep = valueEnd - 1
	}
	if last == 0 {
		return s
	}
	out.WriteString(s[last:])
	return out.String()
}

func credentialKeyByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_' || b == '-' || b == '.'
}

func asciiSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f'
}

func authorizationKey(key string) bool {
	upper := strings.ToUpper(key)
	return upper == "AUTHORIZATION" || strings.HasSuffix(upper, "-AUTHORIZATION") || strings.HasSuffix(upper, "_AUTHORIZATION") || strings.HasSuffix(upper, ".AUTHORIZATION")
}

func credentialTextKeySensitive(key string) bool {
	upper := strings.ToUpper(key)
	compact := strings.NewReplacer("_", "", "-", "").Replace(upper)
	return authorizationKey(key) ||
		strings.Contains(compact, "APIKEY") ||
		strings.Contains(compact, "ACCESSKEY") ||
		strings.Contains(compact, "PRIVATEKEY") ||
		strings.Contains(upper, "SECRET") ||
		strings.Contains(upper, "TOKEN") ||
		strings.Contains(upper, "PASSWORD") ||
		strings.Contains(upper, "PASSWD") ||
		strings.Contains(upper, "_PWD") ||
		strings.Contains(upper, "-PWD")
}

func authorizationScheme(s string) bool {
	switch strings.ToLower(s) {
	case "bearer", "basic", "digest", "negotiate", "ntlm", "token", "bot", "apikey":
		return true
	default:
		return false
	}
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
