package secrets

import (
	"net/url"
	"strings"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
	"reasonix/internal/base/shellparse"
)

const EndpointRedacted = "<redacted>"
const projectionDepth = 32

func CredentialKey(key string) bool {
	for depth := 0; ; depth++ {
		if depth >= projectionDepth {
			return true
		}
		decoded, err := url.QueryUnescape(key)
		if err != nil {
			return true
		}
		if decoded == key {
			break
		}
		key = decoded
	}
	runes := []rune(norm.NFKC.String(key))
	var normalized strings.Builder
	for i, r := range runes {
		if unicode.IsUpper(r) && i > 0 && (unicode.IsLower(runes[i-1]) || (i+1 < len(runes) && unicode.IsLower(runes[i+1]))) {
			normalized.WriteByte(' ')
		}
		normalized.WriteRune(r)
	}
	folded := cases.Fold().String(normalized.String())
	if folded == "pwd" || folded == "oldpwd" {
		return false
	}
	for _, part := range strings.FieldsFunc(folded, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		switch part {
		case "token", "password", "passwd", "pwd", "secret", "key", "auth", "authorization", "credential", "credentials", "signature", "sig", "session", "cookie", "bearer", "jwt", "apikey", "access", "private":
			return true
		}
	}
	return false
}

func RedactEndpoint(raw string) string { return projectEndpoint(raw, 0) }

func projectEndpoint(raw string, depth int) string {
	if strings.TrimSpace(raw) == "" || raw == EndpointRedacted {
		return raw
	}
	if depth >= projectionDepth {
		return EndpointRedacted
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.Scheme == "" || u.Opaque != "" {
		return EndpointRedacted
	}
	changed := u.User != nil
	u.User = nil
	segments := strings.Split(u.Path, "/")
	for i, segment := range segments {
		if CredentialKey(segment) && i+1 < len(segments) {
			u.Path = strings.Join(segments[:i+1], "/") + "/" + EndpointRedacted
			u.RawPath = ""
			changed = true
			break
		}
	}
	if strings.Contains(u.RawQuery, ";") {
		return u.Scheme + "://" + u.Host + "/" + EndpointRedacted
	}
	query, ok := projectQuery(u.RawQuery, depth+1)
	if !ok {
		return EndpointRedacted
	}
	if query != u.RawQuery {
		u.RawQuery = query
		changed = true
	}
	if u.Fragment != "" {
		fragment := EndpointRedacted
		if strings.Contains(u.Fragment, "=") {
			fragment, ok = projectQuery(u.Fragment, depth+1)
			if !ok {
				return EndpointRedacted
			}
		} else if strings.Contains(u.Fragment, "://") {
			fragment = projectEndpoint(u.Fragment, depth+1)
		}
		if fragment != u.Fragment {
			u.Fragment = fragment
			u.RawFragment = ""
			changed = true
		}
	}
	if !changed {
		return raw
	}
	return u.String()
}

func projectQuery(raw string, depth int) (string, bool) {
	if raw == "" {
		return raw, true
	}
	if depth >= projectionDepth {
		return EndpointRedacted, true
	}
	query, err := url.ParseQuery(raw)
	if err != nil {
		return "", false
	}
	changed := false
	out := make(url.Values, len(query))
	for key, values := range query {
		projectedKey := projectValue(key, depth+1)
		for _, value := range values {
			projected := EndpointRedacted
			if !CredentialKey(key) {
				projected = projectValue(value, depth+1)
			}
			if value == "" {
				projected = ""
			}
			changed = changed || projected != value || projectedKey != key
			out[projectedKey] = append(out[projectedKey], projected)
		}
	}
	if !changed {
		return raw, true
	}
	return out.Encode(), true
}

func CredentialValue(value string) bool { return RedactConfigValue("", value) != value }

func RedactConfigValue(key, value string) string {
	if CredentialKey(key) {
		return EndpointRedacted
	}
	return projectValue(value, 0)
}

func projectValue(value string, depth int) string {
	if value == "" || value == EndpointRedacted {
		return value
	}
	if depth >= projectionDepth {
		return EndpointRedacted
	}
	trimmed := strings.TrimSpace(value)
	if strings.Contains(trimmed, "://") && !strings.ContainsFunc(trimmed, unicode.IsSpace) && strings.Index(trimmed, "://") < strings.Index(trimmed+"=", "=") {
		return projectEndpoint(value, depth+1)
	}
	if key, nested, ok := strings.Cut(trimmed, ":"); ok {
		if CredentialKey(key) || projectValue(nested, depth+1) != nested {
			return EndpointRedacted
		}
	}
	if key, _, ok := strings.Cut(trimmed, "="); ok && CredentialKey(key) && !strings.Contains(key, "://") {
		return EndpointRedacted
	}
	if strings.Contains(trimmed, "=") && !strings.ContainsFunc(trimmed, unicode.IsSpace) {
		projected, ok := projectQuery(trimmed, depth+1)
		if !ok {
			return EndpointRedacted
		}
		if projected != trimmed {
			return projected
		}
	}
	if decoded, err := url.QueryUnescape(value); err != nil {
		return EndpointRedacted
	} else if decoded != value && strings.Contains(value, "%") {
		projected := projectValue(decoded, depth+1)
		if projected != decoded {
			return projected
		}
	}
	if strings.ContainsFunc(trimmed, unicode.IsSpace) {
		normalized := strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return ' '
			}
			return r
		}, trimmed)
		command, err := shellparse.ParseStaticCommand(normalized, shellparse.StaticCommandPolicy{})
		if err != nil {
			return EndpointRedacted
		}
		if len(command.Argv) > 1 {
			projected := projectArgs(command.Argv, depth+1)
			for i := range projected {
				if projected[i] != command.Argv[i] {
					return EndpointRedacted
				}
			}
		}
	}
	return value
}

func RedactConfigMap(fields map[string]string) map[string]string {
	if fields == nil {
		return nil
	}
	out := make(map[string]string, len(fields))
	for key, value := range fields {
		out[key] = RedactConfigValue(key, value)
	}
	return out
}

func RedactArgs(args []string) []string { return projectArgs(args, 0) }

func projectArgs(args []string, depth int) []string {
	out := append([]string(nil), args...)
	for i := 0; i < len(out); i++ {
		if depth >= projectionDepth {
			out[i] = EndpointRedacted
			continue
		}
		arg := out[i]
		if len(arg) > 2 && (strings.HasPrefix(arg, "-H") || strings.HasPrefix(arg, "-e")) {
			out[i] = arg[:2] + EndpointRedacted
			continue
		}
		key, _, inline := strings.Cut(arg, "=")
		carrier := key == "-H" || key == "--header" || key == "--headers" || key == "--env" || key == "-e"
		if header, _, ok := strings.Cut(arg, ":"); ok && CredentialKey(header) && strings.HasSuffix(arg, ":") {
			for j := i; j < len(out); j++ {
				out[j] = EndpointRedacted
			}
			break
		}
		flag := strings.HasPrefix(key, "-") && CredentialKey(strings.TrimLeft(key, "-"))
		scheme := strings.EqualFold(key, "Basic") || strings.EqualFold(key, "Bearer")
		if carrier || flag || scheme {
			if strings.ContainsFunc(arg, unicode.IsSpace) {
				out[i] = EndpointRedacted
			}
			if inline {
				out[i] = key + "=" + EndpointRedacted
			} else if i+1 < len(out) {
				i++
				out[i] = EndpointRedacted
			}
			continue
		}
		out[i] = projectValue(arg, depth+1)
	}
	return out
}
