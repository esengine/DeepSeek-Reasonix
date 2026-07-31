package udiff

// UnifiedDiff is a unified diff.
type UnifiedDiff = unified

// Hunk represents a single hunk in a unified diff.
type Hunk = hunk

// Line represents a single line in a hunk.
type Line = line

// ToUnifiedDiff takes a file contents and a sequence of edits, and calculates
// a unified diff that represents those edits.
func ToUnifiedDiff(fromName, toName string, content string, edits []Edit, contextLines int) (UnifiedDiff, error) {
	return toUnified(fromName, toName, content, edits, contextLines)
}
