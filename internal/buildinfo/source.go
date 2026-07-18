// Package buildinfo provides source identity shared by the CLI and Desktop
// artifacts. Product version and Remote schema identity remain protocol data;
// this package only resolves the VCS revision embedded by the Go toolchain or
// explicitly injected by release builds.
package buildinfo

import (
	"runtime/debug"
	"strings"
)

// SourceRevision may be injected with:
//
//	-X reasonix/internal/buildinfo.SourceRevision=<commit>
//
// When empty, Revision falls back to Go's vcs.revision build setting.
var SourceRevision string

// Revision returns the build's source revision. A modified development build
// receives a +dirty suffix so it cannot impersonate a clean build from the same
// commit. It returns an empty string when neither ldflags nor Go build metadata
// provides a revision; Remote BuildID validation rejects that case.
func Revision() string {
	if revision := strings.TrimSpace(SourceRevision); revision != "" {
		return revision
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	return revisionFromSettings(info.Settings)
}

func revisionFromSettings(settings []debug.BuildSetting) string {
	var revision string
	modified := false
	for _, setting := range settings {
		switch setting.Key {
		case "vcs.revision":
			revision = strings.TrimSpace(setting.Value)
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if revision != "" && modified {
		return revision + "+dirty"
	}
	return revision
}
