package doctor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/store"
)

type sessionDirectory struct {
	path    string
	project bool
}

func collectSessions(cwd string) SessionsReport {
	report := SessionsReport{Dir: config.SessionDir()}
	var failures []error
	for _, directory := range knownSessionDirectories(cwd) {
		count, bytes, err := collectSessionDirectory(directory.path)
		report.Count += count
		report.Bytes += bytes
		if directory.project {
			report.ProjectCount += count
			report.ProjectDirectories++
		} else {
			report.GlobalCount += count
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", directory.path, err))
		}
	}
	if err := errors.Join(failures...); err != nil {
		report.Error = err.Error()
	}
	return report
}

func knownSessionDirectories(cwd string) []sessionDirectory {
	type project struct {
		Root string `json:"root"`
	}
	type projectFile struct {
		Projects []project `json:"projects"`
	}

	home := config.ReasonixHomeDir()
	var saved projectFile
	if data, err := os.ReadFile(filepath.Join(home, "desktop-projects.json")); err == nil {
		_ = json.Unmarshal(data, &saved)
	}

	seen := map[string]bool{}
	directories := make([]sessionDirectory, 0, len(saved.Projects)+3)
	add := func(path string, project bool) {
		path = filepath.Clean(strings.TrimSpace(path))
		if path == "." || path == "" {
			return
		}
		key := path
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		if seen[key] {
			return
		}
		seen[key] = true
		directories = append(directories, sessionDirectory{path: path, project: project})
	}

	add(config.SessionDir(), false)
	add(config.ProjectSessionDir(filepath.Join(home, "global-workspace")), false)
	for _, savedProject := range saved.Projects {
		add(config.ProjectSessionDir(savedProject.Root), true)
	}
	add(config.ProjectSessionDir(cwd), true)
	return directories
}

func collectSessionDirectory(dir string) (int, int64, error) {
	sessions, listErr := agent.ListSessions(dir)
	var bytes int64
	walkErr := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		name := filepath.Base(path)
		if !store.IsSessionTranscriptName(name) &&
			!strings.HasSuffix(name, ".events.jsonl") &&
			!strings.HasSuffix(name, ".event-index.json") {
			return nil
		}
		if info, statErr := entry.Info(); statErr == nil {
			bytes += info.Size()
		}
		return nil
	})
	if os.IsNotExist(walkErr) {
		walkErr = nil
	}
	return len(sessions), bytes, errors.Join(listErr, walkErr)
}
