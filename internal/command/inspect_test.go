package command

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestGuessName covers the path → command-name derivation: relative to root,
// trailing .md stripped, subdirectories turned into ":" namespaces.
func TestGuessName(t *testing.T) {
	root := t.TempDir()

	cases := []struct {
		name string
		rel  string // path relative to root; "" means the root itself
		want string
	}{
		{"top level markdown", "review.md", "review"},
		{"nested namespace", filepath.Join("git", "commit.md"), "git:commit"},
		{"deep nesting", filepath.Join("a", "b", "c", "deep.md"), "a:b:c:deep"},
		{"non markdown extension", "notes.txt", "notes.txt"},
		{"uppercase extension kept", "README.MD", "README.MD"},
		{"md suffix inside directory kept", filepath.Join("foo.md", "bar.md"), "foo.md:bar"},
		{"root itself", "", "."},
		{"no extension", "readme", "readme"},
		{"no extension nested", filepath.Join("git", "commit"), "git:commit"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := root
			if tc.rel != "" {
				path = filepath.Join(root, tc.rel)
			}
			if got := guessName(root, path); got != tc.want {
				t.Errorf("guessName(%q, %q) = %q, want %q", root, path, got, tc.want)
			}
		})
	}
}

// TestParseFile covers reading one command file: name derivation, frontmatter
// fields, body handling, and the error path for unreadable files.
func TestParseFile(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "review.md", "---\ndescription: Review the diff\nargument-hint: [area]\n---\nReview, focus on $ARGUMENTS.")
	write(t, dir, "plain.md", "No frontmatter, just $1.")
	write(t, dir, "git/commit.md", "---\ndescription: Commit\n---\nWrite a commit message.")
	write(t, dir, "crlf.md", "---\r\ndescription: CRLF\r\n---\r\nline one\r\nline two")
	write(t, dir, "bom.md", string(rune(0xFEFF))+"---\ndescription: BOM\n---\nBody with BOM.")
	write(t, dir, "pad.md", "\n\n  body text  \n\n")
	write(t, dir, "empty.md", "")
	write(t, dir, "upper.MD", "---\ndescription: Upper\n---\nUPPER BODY")

	cases := []struct {
		name    string
		rel     string
		wantCmd Command
		wantErr bool
	}{
		{
			name: "full frontmatter",
			rel:  "review.md",
			wantCmd: Command{
				Name:        "review",
				Description: "Review the diff",
				ArgHint:     "[area]",
				Body:        "Review, focus on $ARGUMENTS.",
			},
		},
		{
			name: "no frontmatter",
			rel:  "plain.md",
			wantCmd: Command{
				Name: "plain",
				Body: "No frontmatter, just $1.",
			},
		},
		{
			name: "nested namespaced name",
			rel:  filepath.Join("git", "commit.md"),
			wantCmd: Command{
				Name:        "git:commit",
				Description: "Commit",
				Body:        "Write a commit message.",
			},
		},
		{
			name: "crlf normalised",
			rel:  "crlf.md",
			wantCmd: Command{
				Name:        "crlf",
				Description: "CRLF",
				Body:        "line one\nline two",
			},
		},
		{
			name: "bom stripped",
			rel:  "bom.md",
			wantCmd: Command{
				Name:        "bom",
				Description: "BOM",
				Body:        "Body with BOM.",
			},
		},
		{
			name: "body whitespace trimmed",
			rel:  "pad.md",
			wantCmd: Command{
				Name: "pad",
				Body: "body text",
			},
		},
		{
			name:    "empty file",
			rel:     "empty.md",
			wantCmd: Command{Name: "empty"},
		},
		{
			name: "uppercase extension kept in name",
			rel:  "upper.MD",
			wantCmd: Command{
				Name:        "upper.MD",
				Description: "Upper",
				Body:        "UPPER BODY",
			},
		},
		{
			name:    "missing file errors",
			rel:     "missing.md",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.rel)
			cmd, err := parseFile(dir, path)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseFile(%q) succeeded, want error", path)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseFile(%q): %v", path, err)
			}
			tc.wantCmd.Source = path
			if cmd != tc.wantCmd {
				t.Errorf("parseFile() = %+v, want %+v", cmd, tc.wantCmd)
			}
			if strings.Contains(cmd.Body, "\r") {
				t.Errorf("body contains CR: %q", cmd.Body)
			}
		})
	}
}
