package config

import (
	"fmt"
	"regexp"
	"strconv"
)

var tomlErrorLinePattern = regexp.MustCompile(`line (\d+)`)

// ConfigLoadError identifies which config file failed to load and where, so
// callers can isolate the failure: a damaged project reasonix.toml must only
// affect its workspace, while a damaged global config falls back to a
// recovery configuration instead of taking down every tab.
type ConfigLoadError struct {
	Path string
	Line int // 1-based when the parser reported one, else 0
	Err  error
}

func newConfigLoadError(path string, err error) error {
	line := 0
	if m := tomlErrorLinePattern.FindStringSubmatch(err.Error()); len(m) == 2 {
		if n, convErr := strconv.Atoi(m[1]); convErr == nil && n > 0 {
			line = n
		}
	}
	return &ConfigLoadError{Path: path, Line: line, Err: err}
}

func (e *ConfigLoadError) Error() string {
	return fmt.Sprintf("config %s: %v", e.Path, e.Err)
}

func (e *ConfigLoadError) Unwrap() error { return e.Err }

// ConfigLoadErrorOf unwraps err and returns the ConfigLoadError when the
// failure is a config file that failed to parse.
func ConfigLoadErrorOf(err error) (*ConfigLoadError, bool) {
	if err == nil {
		return nil, false
	}
	for err != nil {
		if cle, ok := err.(*ConfigLoadError); ok {
			return cle, true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return nil, false
		}
		err = u.Unwrap()
	}
	return nil, false
}
