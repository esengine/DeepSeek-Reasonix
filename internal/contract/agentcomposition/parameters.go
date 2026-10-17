package agentcomposition

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reasonix/internal/base/secrets"
	"regexp"
	"strings"
)

type Parameters struct {
	Args                  *[]string         `json:"args,omitempty"`
	Env                   map[string]string `json:"env,omitempty"`
	Headers               map[string]string `json:"headers,omitempty"`
	URL                   *string           `json:"url,omitempty"`
	CallTimeoutSeconds    *int              `json:"callTimeoutSeconds,omitempty"`
	StartupTimeoutSeconds *int              `json:"startupTimeoutSeconds,omitempty"`
	TimeoutMillis         *int              `json:"timeoutMillis,omitempty"`
}

var environmentReference = regexp.MustCompile(`^\$\{[a-zA-Z_][a-zA-Z0-9_]*\}$`)

func ParseParameters(raw json.RawMessage) (Parameters, error) {
	var p Parameters
	if len(raw) == 0 {
		return p, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return p, fmt.Errorf("%w: module parameters: %w", ErrInvalid, err)
	}
	for _, pairs := range []map[string]string{p.Env, p.Headers} {
		for _, value := range pairs {
			if !environmentReference.MatchString(value) {
				return p, fmt.Errorf("%w: environment and header values must be ${NAME} references", ErrInvalid)
			}
		}
	}
	if p.CallTimeoutSeconds != nil && *p.CallTimeoutSeconds < 0 || p.StartupTimeoutSeconds != nil && *p.StartupTimeoutSeconds < 0 || p.TimeoutMillis != nil && *p.TimeoutMillis < 0 {
		return p, fmt.Errorf("%w: negative timeout", ErrInvalid)
	}
	if p.Args != nil {
		projected := secrets.RedactArgs(*p.Args)
		for i, arg := range *p.Args {
			_, value, inline := strings.Cut(arg, "=")
			if projected[i] != arg && !environmentReference.MatchString(arg) && !(inline && environmentReference.MatchString(value)) {
				return p, fmt.Errorf("%w: credentials require references", ErrInvalid)
			}
		}
	}
	if p.URL != nil && !environmentReference.MatchString(*p.URL) && secrets.RedactEndpoint(*p.URL) != *p.URL {
		return p, fmt.Errorf("%w: endpoint credentials require references", ErrInvalid)
	}
	return p, nil
}
