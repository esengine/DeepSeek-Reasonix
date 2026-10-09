package gitcmd

import (
	"context"
	"errors"
	"testing"
)

func TestOptionLookingValuesAreRefusedAtTheExit(t *testing.T) {
	cases := [][]string{
		{"clone", "--upload-pack=touch x", "https://example.test/r.git"},
		{"clone", "--upload-pa=touch x", "https://example.test/r.git"},
		{"clone", "-utouch x", "https://example.test/r.git"},
		{"ls-remote", "--upload-pack", "sh", "origin"},
		{"-C", "d", "fetch", "--upload-pack=sh", "origin"},
		{"diff", "--output=/tmp/x", "HEAD"},
		{"diff", "--ext-diff", "HEAD"},
		{"grep", "-Osh", "x"},
	}
	for _, args := range cases {
		cmd := Command(context.Background(), "", args...)
		if !errors.Is(cmd.Err, ErrOptionRefused) {
			t.Errorf("git %v: Err = %v, want ErrOptionRefused", args, cmd.Err)
		}
		if err := cmd.Run(); !errors.Is(err, ErrOptionRefused) {
			t.Errorf("git %v: Run = %v, want ErrOptionRefused", args, err)
		}
		if err := (Repo{}).Command(context.Background(), args...).Run(); err == nil {
			t.Errorf("git %v: unresolved Repo ran", args)
		}
	}
}

func TestOperandsAfterDoubleDashAndOrdinaryFlagsPass(t *testing.T) {
	cases := [][]string{
		{"diff", "--no-color", "HEAD", "--", "--output=x"},
		{"clone", "--depth=1", "--branch", "main", "https://example.test/r.git", "d"},
		{"fetch", "--depth=1", "--", "origin", "--upload-pack=x"},
		{"log", "--oneline", "-n", "3"},
		{"ls-files", "-u"},
		{"-C", "d", "commit", "-c", "HEAD"},
	}
	for _, args := range cases {
		got, err := screen(args)
		if err != nil {
			t.Errorf("git %v: refused: %v", args, err)
		}
		if len(got) != len(args) {
			t.Errorf("git %v: screened to %v", args, got)
		}
	}
}

func TestDashedDirectoryIsSpelledAsPathNotOption(t *testing.T) {
	got := Args("-x/repo", nil, "status")
	for i, a := range got {
		if a == "-C" {
			if got[i+1] != "./-x/repo" {
				t.Fatalf("-C value = %q, want ./-x/repo", got[i+1])
			}
			return
		}
	}
	t.Fatalf("no -C in %v", got)
}

func TestOrdinaryDirectoryIsUntouched(t *testing.T) {
	got := Args("/work/repo", nil, "status")
	for i, a := range got {
		if a == "-C" && got[i+1] != "/work/repo" {
			t.Fatalf("-C value = %q", got[i+1])
		}
	}
}
