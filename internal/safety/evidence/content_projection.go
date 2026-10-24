package evidence

import (
	"context"
	"path"
	"strings"

	"reasonix/internal/base/fileutil"
)

// CaptureProjectedContentSnapshot computes the supported source postimage
// without writing it. Existing directories, ignored files and empty directories
// stay in the input. Unsupported parent creation or links remain unknown.
func CaptureProjectedContentSnapshot(ctx context.Context, root string, updates map[string]fileutil.ContentVersion, limit int) ContentSnapshot {
	result, versions := captureContentVersions(ctx, root, limit)
	if result.Reason != "" {
		return result
	}
	for name, version := range updates {
		if name == "." || name == "" || path.Clean(name) != name || strings.HasPrefix(name, "/") || strings.HasPrefix(name, "../") || strings.ContainsAny(name, "\\:\x00") {
			result.Reason = "invalid_projected_path"
			return result
		}
		for component := range strings.SplitSeq(name, "/") {
			if strings.EqualFold(component, ".git") {
				result.Reason = "projected_git_metadata"
				return result
			}
		}
		if version.Kind == "absent" {
			if prior := versions[name]; prior.Kind == "directory" {
				result.Reason = "unsupported_projected_directory"
				return result
			}
			delete(versions, name)
			continue
		}
		if version.Kind != "file" || version.Digest == "" {
			result.Reason = "unsupported_projected_file"
			return result
		}
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if versions[parent].Kind != "directory" {
				result.Reason = "unsupported_projected_parent"
				return result
			}
		}
		if versions[name].Kind == "directory" {
			result.Reason = "unsupported_projected_directory"
			return result
		}
		versions[name] = version
	}
	if len(versions) > limit || ctx.Err() != nil {
		result.Reason = "projected_input_limit_or_cancelled"
		return result
	}
	return digestContentVersions(result, versions)
}
