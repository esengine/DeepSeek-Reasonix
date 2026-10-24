package boot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"reasonix/internal/base/fileutil"
)

func TestNestedSourceAuthorizationPreservesEveryAuthorityAndMapsOnlyLocalPaths(t *testing.T) {
	for _, mode := range []string{"valid", "execution_revoked", "current_denied", "ancestor_denied", "missing_execution", "missing_ancestor", "outside"} {
		t.Run(mode, func(t *testing.T) {
			source, candidate := t.TempDir(), t.TempDir()
			var calls []string
			denied := errors.New("revoked fixture authority")
			validate := func(context.Context) error {
				calls = append(calls, "execution")
				if mode == "execution_revoked" {
					return denied
				}
				return nil
			}
			current := func(_ context.Context, paths []string) error {
				calls = append(calls, "current")
				if !slices.Equal(paths, []string{filepath.Join(candidate, "file.txt")}) {
					t.Fatalf("direct source path was replaced: %v", paths)
				}
				if mode == "current_denied" {
					return denied
				}
				return nil
			}
			ancestor := func(_ context.Context, paths []string) error {
				calls = append(calls, "ancestor")
				want, err := fileutil.CanonicalWritePath(filepath.Join(source, "file.txt"))
				if err != nil || !slices.Equal(paths, []string{want}) {
					t.Fatalf("wrong ancestor projection: %v %v", paths, err)
				}
				if mode == "ancestor_denied" {
					return denied
				}
				return nil
			}
			if mode == "missing_execution" {
				validate = nil
			}
			if mode == "missing_ancestor" {
				ancestor = nil
			}
			path := filepath.Join(candidate, "file.txt")
			if mode == "outside" {
				path = filepath.Join(t.TempDir(), "file.txt")
			}
			authorize := nestedSourceAuthorizer(candidate, source, current, ancestor, validate)
			err := authorize(t.Context(), []string{path})
			if mode == "valid" {
				if err != nil || !slices.Equal(calls, []string{"execution", "current", "ancestor"}) {
					t.Fatalf("authority chain incomplete: %v %v", calls, err)
				}
			} else if err == nil {
				t.Fatalf("nested source bypassed %s authority", mode)
			}
		})
	}
}

func TestNestedSourceProjectionUsesLiteralHostPaths(t *testing.T) {
	for _, name := range []string{"app/[slug]/page.tsx", "literal?/file.txt"} {
		t.Run(name, func(t *testing.T) {
			if runtime.GOOS == "windows" && name == "literal?/file.txt" {
				t.Skip("question mark is not a valid Windows filename")
			}
			source, candidate := t.TempDir(), t.TempDir()
			got, err := mapIntegrationSourcePaths(candidate, source, []string{filepath.Join(candidate, name)})
			want, wantErr := fileutil.CanonicalWritePath(filepath.Join(source, name))
			if err != nil || wantErr != nil || !slices.Equal(got, []string{want}) {
				t.Fatalf("literal path was treated as declaration syntax: %v %v %v", got, err, wantErr)
			}
		})
	}
}

func TestNestedSourceProjectionRejectsAncestorSymlinkEscape(t *testing.T) {
	source, candidate, outside := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(source, "linked")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := mapIntegrationSourcePaths(candidate, source, []string{filepath.Join(candidate, "linked", "file.txt")}); err == nil {
		t.Fatal("nested source projection escaped through an ancestor symlink")
	}
}
