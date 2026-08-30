package cli

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"reasonix/internal/agentpreset"
)

var deprecatedModeNoticeOnce sync.Once

// acceptDeprecatedModeFlag validates a role value; the caller applies the
// returned floor to the controller. Unknown values error; light folds to
// standard silently.
func acceptDeprecatedModeFlag(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	if _, err := parseRuntimeProfile(raw); err != nil {
		return err
	}
	deprecatedModeNoticeOnce.Do(func() {
		fmt.Fprintln(os.Stderr, agentpreset.FloorNotice)
	})
	return nil
}

// consumeDeprecatedModeFlags strips hidden one-version compatibility flags so
// public help and completions expose only standard execution.
func consumeDeprecatedModeFlags(args []string, names ...string) ([]string, string, error) {
	allowed := make(map[string]struct{}, len(names))
	values := make(map[string]string, len(names))
	for _, name := range names {
		allowed[name] = struct{}{}
	}
	clean := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			clean = append(clean, args[i:]...)
			break
		}
		flagText := ""
		switch {
		case strings.HasPrefix(arg, "--"):
			flagText = strings.TrimPrefix(arg, "--")
		case strings.HasPrefix(arg, "-"):
			flagText = strings.TrimPrefix(arg, "-")
		default:
			clean = append(clean, arg)
			continue
		}
		name, value, hasValue := strings.Cut(flagText, "=")
		if _, ok := allowed[name]; !ok {
			clean = append(clean, arg)
			continue
		}
		if !hasValue {
			if i+1 >= len(args) {
				return nil, "", fmt.Errorf("flag needs an argument: --%s", name)
			}
			i++
			value = args[i]
		}
		values[name] = value
	}
	for _, name := range names {
		if value := strings.TrimSpace(values[name]); value != "" {
			return clean, value, nil
		}
	}
	return clean, "", nil
}
