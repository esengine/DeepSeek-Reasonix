package evidence

import (
	"context"
	"runtime"
	"strings"

	"reasonix/internal/base/fileutil"
)

// ContentBaseline retains versions needed to distinguish publishable changes
// from ignored outputs. Unknown observations never authorize automatic cleanup.
type ContentBaseline struct {
	Snapshot ContentSnapshot                    `json:"snapshot"`
	Versions map[string]fileutil.ContentVersion `json:"versions,omitempty"`
}

func CaptureContentBaseline(ctx context.Context, root string, limit int) ContentBaseline {
	result, versions := captureContentVersions(ctx, root, limit)
	if result.Reason != "" {
		return ContentBaseline{Snapshot: result}
	}
	return ContentBaseline{Snapshot: digestContentVersions(result, versions), Versions: versions}
}

// OnlyPathsChanged compares the full supported domain, including ignored
// paths outside declared dependency roots. New ordinary parent directories
// belong to their published files; other directory changes remain unpublished.
func (b *ContentBaseline) OnlyPathsChanged(ctx context.Context, root string, paths []string, limit int) bool {
	if b == nil || !b.Snapshot.Complete || b.Snapshot.Scope != "workspace-content-v1" || len(b.Versions) == 0 {
		return false
	}
	observed := digestContentVersions(ContentSnapshot{Scope: b.Snapshot.Scope}, b.Versions)
	if observed.Digest != b.Snapshot.Digest {
		return false
	}
	current := CaptureContentBaseline(ctx, root, limit)
	if !current.Snapshot.Complete {
		return false
	}
	allowed := make(map[string]bool, len(paths))
	for _, path := range paths {
		allowed[path] = true
	}
	for path, before := range b.Versions {
		if !allowed[path] && current.Versions[path] != before {
			return false
		}
	}
	for path, after := range current.Versions {
		if _, exists := b.Versions[path]; exists || allowed[path] {
			continue
		}
		ordinaryMode := uint32(0755)
		if runtime.GOOS == "windows" {
			ordinaryMode = 0666
		}
		if after.Kind != "directory" || after.Mode != ordinaryMode || !newPublishedParent(path, paths) {
			return false
		}
	}
	return true
}

func newPublishedParent(path string, paths []string) bool {
	for _, changed := range paths {
		if strings.HasPrefix(changed, path+"/") {
			return true
		}
	}
	return false
}
