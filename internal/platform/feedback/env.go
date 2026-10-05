package feedback

import (
	"os"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"

	"reasonix/internal/contract/provider"
)

var stableVersion = regexp.MustCompile(`^v?\d+\.\d+\.\d+$`)

// CollectEnv describes this install for a report. The version comes from the
// host that declared it; everything else is read from the running process.
func CollectEnv(ctx EnvContext) Env {
	return Env{
		Version:      clamp(displayVersion(provider.ClientVersion()), 40),
		Commit:       clamp(buildCommit(), 40),
		Surface:      string(ctx.Surface),
		OS:           clamp(runtime.GOOS, 40),
		OSVersion:    clamp(osVersion(), 80),
		Arch:         clamp(runtime.GOARCH, 20),
		Locale:       clamp(localeOf(ctx.Locale), 20),
		Channel:      channelOf(provider.ClientVersion()),
		ProviderKind: providerKind(ctx.ProviderKind),
	}
}

func displayVersion(v string) string {
	v = strings.TrimSpace(v)
	if v != "" && v[0] >= '0' && v[0] <= '9' {
		return "v" + v
	}
	return v
}

func channelOf(v string) string {
	if stableVersion.MatchString(strings.TrimSpace(v)) {
		return "stable"
	}
	return "preview"
}

func providerKind(kind string) string {
	switch kind {
	case "deepseek", "openai", "anthropic":
		return kind
	}
	return "other"
}

func buildCommit() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			return s.Value[:min(len(s.Value), 7)]
		}
	}
	return ""
}

func localeOf(given string) string {
	if tag := localeTag(given); tag != "" {
		return tag
	}
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if tag := localeTag(os.Getenv(key)); tag != "" {
			return tag
		}
	}
	return "en"
}

func localeTag(raw string) string {
	raw, _, _ = strings.Cut(strings.TrimSpace(raw), ".")
	raw = strings.ReplaceAll(raw, "_", "-")
	if raw == "" || raw == "C" || raw == "POSIX" {
		return ""
	}
	return raw
}

// clamp keeps a field within the service's schema, on a rune boundary.
func clamp(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}
