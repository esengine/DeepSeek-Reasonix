package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/installlayout"
)

func TestLinuxShellRejectsExpandedSizeOverflow(t *testing.T) {
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "reasonix", Mode: 0755, Size: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := tw.WriteHeader(&tar.Header{Name: "app/large", Mode: 0644, Size: math.MaxInt64}); err != nil {
		t.Fatal(err)
	}
	// The oversized member has no body: the limit must reject its header
	// before attempting to copy, even when the summed sizes would overflow.
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	_, _, err := stageLinuxShellRelease(archive.Bytes(), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "extraction limit") {
		t.Fatalf("oversized header must fail before body copy, got %v", err)
	}
}

func linuxShellArchive(t *testing.T, extra *tar.Header, omit string) []byte {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	names := append([]string{"reasonix-desktop", "reasonix", "reasonix-launcher", "reasonix-guard"}, installlayout.ShellRequiredNames("linux")...)
	names = append(names, "app/chrome-sandbox", "app/locales/en-US.pak")
	for _, name := range names {
		if name == omit {
			continue
		}
		data := []byte("new-" + name)
		h := &tar.Header{Name: name, Mode: 0755, Size: int64(len(data)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if extra != nil {
		if err := tw.WriteHeader(extra); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestLinuxShellUpdateAtomicallyPublishesResourcesAndLauncher(t *testing.T) {
	root := t.TempDir()
	archive := linuxShellArchive(t, nil, "")
	for _, version := range []string{"v1.39.0", "v1.39.1"} {
		if err := activateLinuxShellRelease(archive, version, root); err != nil {
			t.Fatal(err)
		}
	}
	ptr, err := installlayout.ReadCurrent(root)
	if err != nil || ptr.ActiveVersion != "v1.39.1" {
		t.Fatalf("pointer=%+v %v", ptr, err)
	}
	for _, name := range append(installlayout.ShellRequiredNames("linux"), "app/locales/en-US.pak", "reasonix-desktop", "reasonix") {
		p := filepath.Join(root, "versions", ptr.ActiveVersion, filepath.FromSlash(name))
		data, err := os.ReadFile(p)
		if err != nil || string(data) != "new-"+name {
			t.Errorf("bad %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "reasonix-launcher")); err != nil {
		t.Fatal(err)
	}
}

func TestLinuxShellUpdateRejectsPartialOrUnsafeTreeWithoutMovingPointer(t *testing.T) {
	cases := map[string]struct {
		header *tar.Header
		omit   string
	}{
		"missing renderer": {nil, "app/resources/app.asar"},
		"traversal":        {&tar.Header{Name: "app/../outside", Typeflag: tar.TypeReg}, ""},
		"symlink":          {&tar.Header{Name: "app/link", Linkname: "/tmp", Typeflag: tar.TypeSymlink}, ""},
		"duplicate":        {&tar.Header{Name: "app/Reasonix", Typeflag: tar.TypeReg}, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := activateLinuxShellRelease(linuxShellArchive(t, nil, ""), "v1.39.0", root); err != nil {
				t.Fatal(err)
			}
			if err := activateLinuxShellRelease(linuxShellArchive(t, tc.header, tc.omit), "v1.39.1", root); err == nil {
				t.Fatal("accepted invalid release")
			}
			ptr, err := installlayout.ReadCurrent(root)
			if err != nil || ptr.ActiveVersion != "v1.39.0" {
				t.Fatalf("changed old pointer: %+v %v", ptr, err)
			}
		})
	}
}

func TestLinuxShellExtractionCannotFollowPreexistingLinksOutsideStaging(t *testing.T) {
	for _, name := range []string{"app", "app/resources", "reasonix-desktop"} {
		t.Run(name, func(t *testing.T) {
			staging, outside := t.TempDir(), t.TempDir()
			sentinel := filepath.Join(outside, "keep")
			if err := os.WriteFile(sentinel, []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			target := outside
			if name == "reasonix-desktop" {
				target = sentinel
			}
			link := filepath.Join(staging, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if _, _, err := stageLinuxShellRelease(linuxShellArchive(t, nil, ""), staging); err == nil {
				t.Fatal("extraction accepted a symlink outside staging")
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 1 || entries[0].Name() != "keep" {
				t.Fatalf("extraction created files outside staging: %v %v", entries, err)
			}
			data, err := os.ReadFile(sentinel)
			if err != nil || string(data) != "original" {
				t.Fatalf("extraction changed outside file: %q %v", data, err)
			}
		})
	}
}

func TestLinuxShellExtractionRejectsEscapingPathsWithoutOutsideWrites(t *testing.T) {
	parent := t.TempDir()
	outside := filepath.Join(parent, "outside")
	for _, name := range []string{"../outside", "app/../../outside", outside, `app\..\..\outside`} {
		t.Run(name, func(t *testing.T) {
			staging, err := os.MkdirTemp(parent, "staging-")
			if err != nil {
				t.Fatal(err)
			}
			archive := linuxShellArchive(t, &tar.Header{Name: name, Typeflag: tar.TypeReg}, "")
			if _, _, err := stageLinuxShellRelease(archive, staging); err == nil {
				t.Fatal("extraction accepted an escaping path")
			}
			if _, err := os.Stat(outside); !os.IsNotExist(err) {
				t.Fatalf("extraction created an outside file: %v", err)
			}
		})
	}
}
