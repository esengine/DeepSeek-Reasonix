package agent

import "testing"

func TestUserPreviewTextStripsReferencedContext(t *testing.T) {
	content := "Referenced context:\n\n" +
		"<file path=\"README.md\">\n# Reasonix\n</file>\n\n" +
		"请总结这个文件"

	if got := UserPreviewText(content); got != "请总结这个文件" {
		t.Fatalf("UserPreviewText = %q, want user prompt", got)
	}
}

func TestUserPreviewTextStripsMultipleReferencedContextBlocks(t *testing.T) {
	content := "Referenced context:\n\n" +
		"<file path=\"a.go\">\npackage a\n</file>\n\n" +
		"<dir path=\"docs\">\nintro.md\n</dir>\n\n" +
		"explain these refs"

	if got := UserPreviewText(content); got != "explain these refs" {
		t.Fatalf("UserPreviewText = %q, want user prompt", got)
	}
}

func TestUserPreviewTextLeavesLiteralReferencedContext(t *testing.T) {
	content := "Referenced context is a phrase the user typed"

	if got := UserPreviewText(content); got != content {
		t.Fatalf("UserPreviewText = %q, want literal prompt", got)
	}
}
