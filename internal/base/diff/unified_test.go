package diff

import "testing"

const sampleGitDiff = "diff --git a/x.go b/x.go\n" +
	"index 112294521..f9fdd2c37 100644\n" +
	"--- a/x.go\n" +
	"+++ b/x.go\n" +
	"@@ -1 +1 @@\n" +
	"-old\n" +
	"+new\n"

// The default `git show` / `git log -p` shape: a commit header and its indented
// message, then the file sections.
const sampleShow = "commit 0f2a1b9c\n" +
	"Author: A <a@b>\n" +
	"Date:   Mon Jan 1 00:00:00 2024 +0000\n" +
	"\n" +
	"    subject line\n" +
	"\n" +
	"    body paragraph\n" +
	"\n" +
	sampleGitDiff

func TestIsUnifiedDiff(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"git-style whole diff", sampleGitDiff, true},
		{"two files", sampleGitDiff + "diff --git a/y.go b/y.go\n--- a/y.go\n+++ b/y.go\n@@ -1 +1 @@\n-a\n+b\n", true},
		{"leading blank lines are skipped", "\n\n" + sampleGitDiff, true},
		{"trailing newline", sampleGitDiff + "\n", true},
		{"--- style header", "--- a/x\n+++ b/x\n@@ -1 +1 @@\n-a\n+b\n", true},
		{"no-newline marker", "diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n-old\n\\ No newline at end of file\n+new\n\\ No newline at end of file\n", true},
		{"zero-count hunk", "--- a/x\n+++ b/x\n@@ -1,2 +1,1 @@\n-a\n-b\n+c\n", true},
		{"git show default format", sampleShow, true},
		{"git show without a message", "commit 0f2a1b9c\nAuthor: A <a@b>\nDate:   now\n\n" + sampleGitDiff, true},
		{"git log -p two commits", sampleShow + "commit 1234567\nAuthor: B <b@b>\nDate:   later\n\n    second\n\n" + sampleGitDiff, true},
		{"git log -p with an empty commit", sampleShow + "commit 89abcde\nAuthor: C <c@c>\nDate:   later\n\n    empty\n\n", true},
		{"empty", "", false},
		{"blank only", "\n \n", false},
		{"plain prose", "Replaced all 15 em dashes in README.md.\n", false},
		{"mixed output", "building...\n" + sampleGitDiff, false},
		{"diff then a command", sampleGitDiff + "ok  \tgithub.com/x/y\t0.02s\n", false},
		{"diff then prose", sampleGitDiff + "That changed the helper.\n", false},
		{"diff then a truncation note", sampleGitDiff + "…(512 more chars truncated)", false},
		{"prose faking headers", "--- Summary ---\n@@ x @@\n- a\n+ b\n", false},
		{"git log header without a diff", "commit 0f2a1b9c\nAuthor: A <a@b>\nDate:   now\n\n    only a message\n", false},
		{"git show --stat, no hunk", "commit 0f2a1b9c\nAuthor: A <a@b>\nDate:   now\n\n    subject\n\n x.go | 2 +-\n 1 file changed, 1 insertion(+), 1 deletion(-)\n", false},
		{"commit line without header fields", "commit 0f2a1b9c\nnot a header field\n\n    subject\n\n" + sampleGitDiff, false},
		{"stat only, no hunk", "diff --git a/x b/x\n x | 2 +-\n 1 file changed, 1 insertion(+), 1 deletion(-)\n", false},
		{"hunk but no changed line", "diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n same\n", false},
		{"no hunk", "diff --git a/x b/x\n--- a/x\n+++ b/x\n-old\n+new\n", false},
		{"hunk count too short", "--- a/x\n+++ b/x\n@@ -1,3 +1,3 @@\n-a\n+b\n", false},
		{"hunk count too long", "--- a/x\n+++ b/x\n@@ -1 +1 @@\n-a\n+b\n+c\n", false},
		{"hunk overshoots its removed count", "--- a/x\n+++ b/x\n@@ -1 +1,2 @@\n-a\n-a\n+b\n+b\n", false},
		{"hunk overshoots its added count", "--- a/x\n+++ b/x\n@@ -1,2 +1 @@\n-a\n+a\n+a\n", false},
		{"--- without +++", "--- a/x\n@@ -1 +1 @@\n-a\n+b\n", false},
		{"garbage in body", "--- a/x\n+++ b/x\n@@ -1 +1 @@\n-a\nnot a diff line\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsUnifiedDiff(tc.in); got != tc.want {
				t.Fatalf("IsUnifiedDiff = %v, want %v", got, tc.want)
			}
		})
	}
}
