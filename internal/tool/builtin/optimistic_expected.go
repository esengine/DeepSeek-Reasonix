package builtin

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// ExpectedContentArg is the optional JSON field name shared by all built-in
// file-writing tools when optimistic concurrency (write-if-unchanged) is on.
//
// When a tool call supplies "expected", the write is rejected unless the
// target file's current content matches it exactly. This lets cooperating
// sessions/calls write in parallel and fall back to a stale-content error
// instead of a whole-workspace serialization lock — mirroring opencode's
// writeIfUnchanged / StaleContentError model. When "expected" is omitted the
// call behaves exactly as before (pessimistic lock path unchanged).
const ExpectedContentArg = "expected"

// expectedContentDescription is the shared schema snippet for the optional
// parameter. It is appended to a writer tool's properties so an explicit
// baseline can be requested.
const expectedContentDescription = "Optional baseline: the file's exact current content as the caller believes it is. When supplied, the write is refused with a stale-content error unless the on-disk content still matches, avoiding clobbering concurrent changes."

func expectedContentSchemaField() string {
	return `"expected":{"type":"string","description":"` + expectedContentDescription + `"}`
}

// checkOptimisticExpected verifies an optional write-if-unchanged baseline.
// It returns nil when the call supplies no "expected" (or the content already
// matches), and a stale-content error when the provided baseline is out of
// date. It is called once the target path is resolved and confined, before the
// write is applied.
func checkOptimisticExpected(ctx context.Context, overlay FileOverlay, path, expected, toolName string) error {
	if strings.TrimSpace(expected) == "" {
		return nil
	}
	src, err := readEditSource(ctx, overlay, path)
	if err != nil {
		// A missing file is a legitimately empty baseline; anything else means
		// we cannot verify, so fall back to the pessimistic path (no optimistic
		// refusal risked on an unreadable target).
		if os.IsNotExist(err) {
			if expected != "" {
				return fmt.Errorf("stale content: %s: file does not exist, but expected content was provided (refusing optimistic write)", path)
			}
			return nil
		}
		return fmt.Errorf("%s: cannot verify expected content for optimistic write: %w", toolName, err)
	}
	if src.content != expected {
		return fmt.Errorf(
			"stale content: %s: the file has changed since the expected baseline (optimistic write aborted). Re-read the current content with read_file and retry with the updated baseline.", path)
	}
	return nil
}
