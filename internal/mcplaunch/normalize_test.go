package mcplaunch

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestUpsertLaunchGrant(t *testing.T) {
	old := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	recent := time.Date(2025, 6, 7, 8, 9, 10, 0, time.UTC)
	existing := LaunchGrant{
		Scope: workspaceScope, WorkspaceFingerprint: "ws-a",
		Server: "project", ConfigSource: "project_config",
		IdentityDigest: "identity-v1", CreatedAt: old,
	}
	updated := existing
	updated.IdentityDigest = "identity-v2"
	updated.CreatedAt = recent
	otherServer := LaunchGrant{
		Scope: workspaceScope, WorkspaceFingerprint: "ws-a",
		Server: "other", ConfigSource: "project_config",
		IdentityDigest: "digest-other", CreatedAt: old,
	}
	kept := updated
	kept.CreatedAt = old // CreatedAt must survive from the matched grant

	cases := []struct {
		name   string
		grants []LaunchGrant
		grant  LaunchGrant
		want   []LaunchGrant
	}{
		{
			name:   "appends when no grant matches",
			grants: []LaunchGrant{otherServer},
			grant:  existing,
			want:   []LaunchGrant{otherServer, existing},
		},
		{
			name:   "replaces matching grant in place",
			grants: []LaunchGrant{otherServer, existing},
			grant:  updated,
			want:   []LaunchGrant{otherServer, kept},
		},
		{
			name:   "appends on empty grant list",
			grants: nil,
			grant:  existing,
			want:   []LaunchGrant{existing},
		},
		{
			name:   "same server and config but other workspace appends",
			grants: []LaunchGrant{{WorkspaceFingerprint: "ws-b", Server: "project", ConfigSource: "project_config", IdentityDigest: "x", CreatedAt: old}},
			grant:  existing,
			want:   []LaunchGrant{{WorkspaceFingerprint: "ws-b", Server: "project", ConfigSource: "project_config", IdentityDigest: "x", CreatedAt: old}, existing},
		},
		{
			name:   "same server and workspace but other config source appends",
			grants: []LaunchGrant{{WorkspaceFingerprint: "ws-a", Server: "project", ConfigSource: "other_config", IdentityDigest: "x", CreatedAt: old}},
			grant:  existing,
			want:   []LaunchGrant{{WorkspaceFingerprint: "ws-a", Server: "project", ConfigSource: "other_config", IdentityDigest: "x", CreatedAt: old}, existing},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := upsertLaunchGrant(tc.grants, tc.grant)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("upsertLaunchGrant = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestDedupeLaunchGrants(t *testing.T) {
	old := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	mk := func(ws, server, config, digest string, at time.Time) LaunchGrant {
		return LaunchGrant{Scope: workspaceScope, WorkspaceFingerprint: ws, Server: server, ConfigSource: config, IdentityDigest: digest, CreatedAt: at}
	}
	first := mk("ws-a", "project", "cfg", "digest-v1", old)
	dup := mk("ws-a", "project", "cfg", "digest-v2", old.Add(time.Hour))
	kept := dup
	kept.CreatedAt = old // first occurrence's CreatedAt wins
	other := mk("ws-a", "other", "cfg", "digest-other", old)

	cases := []struct {
		name   string
		grants []LaunchGrant
		want   []LaunchGrant
	}{
		{
			name:   "empty input yields empty non-nil slice",
			grants: nil,
			want:   []LaunchGrant{},
		},
		{
			name:   "exact duplicates collapse keeping first CreatedAt",
			grants: []LaunchGrant{first, dup, other},
			want:   []LaunchGrant{kept, other},
		},
		{
			name:   "later duplicate keeps first occurrence position",
			grants: []LaunchGrant{other, first, dup},
			want:   []LaunchGrant{other, kept},
		},
		{
			name:   "same server with other workspace or config source survives",
			grants: []LaunchGrant{first, mk("ws-b", "project", "cfg", "d", old), mk("ws-a", "project", "cfg2", "d", old), dup},
			want:   []LaunchGrant{kept, mk("ws-b", "project", "cfg", "d", old), mk("ws-a", "project", "cfg2", "d", old)},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := dedupeLaunchGrants(tc.grants)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("dedupeLaunchGrants = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestNormalizeTransport(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "stdio"},
		{"STDIO", "stdio"},
		{"http", "http"},
		{"streamable-http", "http"},
		{"streamable_http", "http"},
		{"sse", "sse"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := normalizeTransport(tc.in); got != tc.want {
				t.Fatalf("normalizeTransport(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestCanonicalPath(t *testing.T) {
	t.Run("empty and blank input stay empty", func(t *testing.T) {
		for _, in := range []string{"", "   "} {
			if got := canonicalPath(in); got != "" {
				t.Fatalf("canonicalPath(%q) = %q, want empty", in, got)
			}
		}
	})

	t.Run("relative path becomes absolute and clean", func(t *testing.T) {
		got := canonicalPath("sub" + string(filepath.Separator) + "dir")
		if !filepath.IsAbs(got) {
			t.Fatalf("canonicalPath(relative) = %q, want absolute", got)
		}
		if got != filepath.Clean(got) {
			t.Fatalf("canonicalPath(relative) = %q, not clean", got)
		}
		if filepath.Base(got) != "dir" {
			t.Fatalf("canonicalPath(relative) = %q, want to end in %q", got, filepath.Join("sub", "dir"))
		}
	})

	t.Run("dot segments are cleaned", func(t *testing.T) {
		dir := t.TempDir()
		got := canonicalPath(filepath.Join(dir, "a", "..", "b"))
		want := canonicalPath(filepath.Join(dir, "b"))
		if got != want {
			t.Fatalf("canonicalPath with dot segments = %q, want %q", got, want)
		}
	})

	t.Run("symlink resolves to real path", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "real-file")
		if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(dir, "link-file")
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlinks unsupported: %v", err)
		}
		got := canonicalPath(link)
		if got == filepath.Clean(link) {
			t.Fatalf("canonicalPath(link) = %q, symlink not resolved", got)
		}
		if got != canonicalPath(target) {
			t.Fatalf("canonicalPath(link) = %q, want %q", got, canonicalPath(target))
		}
		if real, err := filepath.EvalSymlinks(dir); err == nil && real == dir {
			// TempDir is not itself behind a symlink, so the resolved
			// path must be byte-for-byte the target.
			if got != target {
				t.Fatalf("canonicalPath(link) = %q, want %q", got, target)
			}
		}
	})

	t.Run("nonexistent path is made absolute and clean without error", func(t *testing.T) {
		dir := t.TempDir()
		got := canonicalPath(filepath.Join(dir, "missing", "..", "x"))
		want := canonicalPath(filepath.Join(dir, "x"))
		if got != want {
			t.Fatalf("canonicalPath(nonexistent) = %q, want %q", got, want)
		}
	})
}

func TestCleanStrings(t *testing.T) {
	t.Run("nil input yields empty non-nil slice", func(t *testing.T) {
		got := cleanStrings(nil, false)
		if got == nil || len(got) != 0 {
			t.Fatalf("cleanStrings(nil) = %#v, want empty non-nil", got)
		}
	})

	t.Run("trims whitespace and drops empty entries", func(t *testing.T) {
		got := cleanStrings([]string{"  b ", "", "a", "   "}, false)
		if want := []string{"a", "b"}; !slices.Equal(got, want) {
			t.Fatalf("cleanStrings = %q, want %q", got, want)
		}
	})

	t.Run("fold lowercases before sorting without mutating input", func(t *testing.T) {
		in := []string{"Zebra", "apple", "Banana"}
		before := slices.Clone(in)
		got := cleanStrings(in, true)
		if want := []string{"apple", "banana", "zebra"}; !slices.Equal(got, want) {
			t.Fatalf("cleanStrings(fold) = %q, want %q", got, want)
		}
		if !slices.Equal(in, before) {
			t.Fatalf("cleanStrings mutated input: got %q, want %q", in, before)
		}
	})

	t.Run("without fold case is preserved", func(t *testing.T) {
		got := cleanStrings([]string{"Zebra", "apple", "Banana"}, false)
		if want := []string{"Banana", "Zebra", "apple"}; !slices.Equal(got, want) {
			t.Fatalf("cleanStrings(no fold) = %q, want %q", got, want)
		}
	})

	t.Run("duplicates collapse after sorting", func(t *testing.T) {
		got := cleanStrings([]string{"b", "a", "b", "a"}, false)
		if want := []string{"a", "b"}; !slices.Equal(got, want) {
			t.Fatalf("cleanStrings(dedupe) = %q, want %q", got, want)
		}
	})
}

func TestCompactStrings(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"nil passes through", nil, nil},
		{"single element passes through", []string{"a"}, []string{"a"}},
		{"no duplicates unchanged", []string{"a", "b"}, []string{"a", "b"}},
		{"consecutive duplicates collapse", []string{"a", "a", "b", "b", "a"}, []string{"a", "b", "a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := compactStrings(tc.in)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("compactStrings(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeState(t *testing.T) {
	at := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	grant := func(server, config string) LaunchGrant {
		return LaunchGrant{Scope: workspaceScope, WorkspaceFingerprint: "ws-a", Server: server, ConfigSource: config, IdentityDigest: "d", CreatedAt: at}
	}
	lock := func(server, locator string) LauncherLock {
		return LauncherLock{Server: server, Locator: locator}
	}

	t.Run("zero version defaults and nonzero version is preserved", func(t *testing.T) {
		for _, v := range []int{0, StoreVersion} {
			state := &State{Version: v}
			normalizeState(state)
			if state.Version != StoreVersion {
				t.Fatalf("version = %d, want %d", state.Version, StoreVersion)
			}
		}
	})

	t.Run("duplicate launch grants collapse", func(t *testing.T) {
		state := &State{LaunchGrants: []LaunchGrant{grant("project", "cfg"), grant("project", "cfg")}}
		normalizeState(state)
		if len(state.LaunchGrants) != 1 {
			t.Fatalf("launch grants = %+v, want 1", state.LaunchGrants)
		}
	})

	t.Run("launch grants sort by server then config source", func(t *testing.T) {
		state := &State{LaunchGrants: []LaunchGrant{
			grant("zeta", "cfg2"), grant("alpha", "cfg1"), grant("zeta", "cfg1"),
		}}
		normalizeState(state)
		want := []LaunchGrant{grant("alpha", "cfg1"), grant("zeta", "cfg1"), grant("zeta", "cfg2")}
		if !slices.Equal(state.LaunchGrants, want) {
			t.Fatalf("launch grants = %+v, want %+v", state.LaunchGrants, want)
		}
	})

	t.Run("launcher locks sort by server then locator", func(t *testing.T) {
		state := &State{LauncherLocks: []LauncherLock{
			lock("srv2", "loc2"), lock("srv1", "loc2"), lock("srv1", "loc1"),
		}}
		normalizeState(state)
		want := []LauncherLock{lock("srv1", "loc1"), lock("srv1", "loc2"), lock("srv2", "loc2")}
		if !slices.Equal(state.LauncherLocks, want) {
			t.Fatalf("launcher locks = %+v, want %+v", state.LauncherLocks, want)
		}
	})
}

func TestFileSHA256(t *testing.T) {
	t.Run("matches sha256 of file content", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "payload")
		body := []byte("hello mcp\n")
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		want := hex.EncodeToString(sum[:])
		got, err := FileSHA256(path)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("FileSHA256 = %q, want %q", got, want)
		}
	})

	t.Run("empty file hashes to sha256 of empty input", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "empty")
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(nil)
		want := hex.EncodeToString(sum[:])
		got, err := FileSHA256(path)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("FileSHA256(empty) = %q, want %q", got, want)
		}
	})

	t.Run("missing file returns error", func(t *testing.T) {
		if _, err := FileSHA256(filepath.Join(t.TempDir(), "nope")); err == nil {
			t.Fatal("FileSHA256(missing) returned nil error")
		}
	})

	t.Run("directory path returns error", func(t *testing.T) {
		if _, err := FileSHA256(t.TempDir()); err == nil {
			t.Fatal("FileSHA256(dir) returned nil error")
		}
	})
}
