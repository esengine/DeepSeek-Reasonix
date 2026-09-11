package evidence

import (
	"path/filepath"
	"strings"
)

// ReceiptShowsWholeGitDiff reports whether a successful shell receipt exposed
// the current repository diff content. Status summaries and diff --check do
// not count as content review.
func ReceiptShowsWholeGitDiff(rec Receipt) bool {
	toolName := strings.ToLower(strings.TrimSpace(rec.ToolName))
	return receiptCommandSucceeded(rec) && (toolName == "bash" || toolName == "shell") && rec.OutputBytes > 0 && commandShowsWholeGitDiff(rec.Command)
}

// ReceiptShowsContentForPath reports whether a successful shell receipt
// exposed the named file's content rather than merely mentioning the path.
func ReceiptShowsContentForPath(rec Receipt, path string) bool {
	toolName := strings.ToLower(strings.TrimSpace(rec.ToolName))
	path = strings.ToLower(filepath.ToSlash(normalizePath(path)))
	return path != "" && receiptCommandSucceeded(rec) &&
		(toolName == "bash" || toolName == "shell") && rec.OutputBytes > 0 &&
		commandShowsContentForPath(rec.Command, path)
}

func receiptCommandSucceeded(rec Receipt) bool {
	return rec.Success && (rec.ExitCode == nil || *rec.ExitCode == 0) && rec.Verification != VerificationFailed
}
