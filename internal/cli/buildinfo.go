package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"reasonix/internal/config"
)

// BuildInfo is the human-facing build identity printed by `reasonix --version`.
// Release builds may only fill Version; source builds can inject the rest with
// -ldflags so the binary is traceable without invoking git at runtime.
type BuildInfo struct {
	Version      string
	BuildNumber  string
	BuildTimeUTC string
	GitCommit    string
	GitDirty     string
	BuildProfile string
	BuildTarget  string
}

func (b BuildInfo) VersionText() string {
	b = b.withDefaults()
	version := strings.TrimSpace(b.Version)
	lines := []string{"reasonix " + version}
	appendField := func(k, v string) {
		v = strings.TrimSpace(v)
		if v != "" {
			lines = append(lines, k+": "+v)
		}
	}
	appendField("build_number", b.BuildNumber)
	appendField("build_time_utc", b.BuildTimeUTC)
	if cst := buildTimeCST(b.BuildTimeUTC); cst != "" {
		appendField("build_time_cst", cst)
	}
	appendField("git_commit", b.GitCommit)
	appendField("git_dirty", b.GitDirty)
	appendField("build_profile", b.BuildProfile)
	appendField("build_target", b.BuildTarget)
	appendField("go", fmt.Sprintf("go version %s %s/%s", runtime.Version(), runtime.GOOS, runtime.GOARCH))
	if path, mode := versionConfigSummary(); path != "" {
		appendField("config_path", path)
		appendField("config_mode", mode)
	}
	return strings.Join(lines, "\n")
}

func (b BuildInfo) withDefaults() BuildInfo {
	if strings.TrimSpace(b.Version) == "" {
		b.Version = "dev"
	}
	if strings.TrimSpace(b.BuildTarget) == "" {
		b.BuildTarget = defaultBuildTarget()
	}
	if strings.TrimSpace(b.BuildProfile) == "" {
		b.BuildProfile = "debug"
	}
	if strings.TrimSpace(b.GitCommit) == "" || strings.TrimSpace(b.GitDirty) == "" {
		if info, ok := debug.ReadBuildInfo(); ok {
			for _, setting := range info.Settings {
				switch setting.Key {
				case "vcs.revision":
					if strings.TrimSpace(b.GitCommit) == "" {
						b.GitCommit = shortGitRevision(setting.Value)
					}
				case "vcs.modified":
					if strings.TrimSpace(b.GitDirty) == "" {
						switch setting.Value {
						case "true":
							b.GitDirty = "dirty"
						case "false":
							b.GitDirty = "clean"
						}
					}
				}
			}
		}
	}
	if strings.TrimSpace(b.GitCommit) == "" {
		b.GitCommit = "unknown"
	}
	if strings.TrimSpace(b.GitDirty) == "" {
		b.GitDirty = "unknown"
	}
	if strings.TrimSpace(b.BuildTimeUTC) == "" {
		b.BuildTimeUTC = "unknown"
	}
	if strings.TrimSpace(b.BuildNumber) == "" {
		if number := buildNumberFromUTC(b.BuildTimeUTC); number != "" {
			b.BuildNumber = number
		} else {
			b.BuildNumber = "dev"
		}
	}
	return b
}

func shortGitRevision(revision string) string {
	revision = strings.TrimSpace(revision)
	if len(revision) > 8 {
		return revision[:8]
	}
	return revision
}

func buildNumberFromUTC(utc string) string {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(utc))
	if err != nil {
		return ""
	}
	return t.UTC().Format("20060102150405")
}

func buildTimeCST(utc string) string {
	utc = strings.TrimSpace(utc)
	if utc == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, utc)
	if err != nil {
		return ""
	}
	cst := time.FixedZone("CST", 8*60*60)
	return t.In(cst).Format("2006-01-02 15:04:05 MST")
}

func defaultBuildTarget() string {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "darwin/arm64":
		return "aarch64-apple-darwin"
	case "darwin/amd64":
		return "x86_64-apple-darwin"
	case "linux/arm64":
		return "aarch64-unknown-linux-gnu"
	case "linux/amd64":
		return "x86_64-unknown-linux-gnu"
	case "windows/amd64":
		return "x86_64-pc-windows-msvc"
	case "windows/arm64":
		return "aarch64-pc-windows-msvc"
	default:
		if runtime.GOOS == "" || runtime.GOARCH == "" {
			return ""
		}
		return runtime.GOARCH + "-" + runtime.GOOS
	}
}

func versionConfigSummary() (string, string) {
	if wd, err := os.Getwd(); err == nil {
		project := filepath.Join(wd, "reasonix.toml")
		if _, err := os.Stat(project); err == nil {
			if abs, err := filepath.Abs(project); err == nil {
				return abs, "project"
			}
			return project, "project"
		}
	}
	user := config.UserConfigPath()
	if user == "" {
		return "", ""
	}
	if _, err := os.Stat(user); err == nil {
		return user, "user"
	}
	return user, "missing"
}
