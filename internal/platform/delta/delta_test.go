package delta

import (
	"bytes"
	"context"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/base/testenv"
)

func noise(seed int64, n int) []byte {
	b := make([]byte, n)
	rand.New(rand.NewSource(seed)).Read(b)
	return b
}

func TestSplitReaderCutsWhereSplitCuts(t *testing.T) {
	data := noise(1, 900_000)
	want := Split(data)
	var got []Span
	if err := SplitReader(bytes.NewReader(data), func(s Span, b []byte) error {
		if !bytes.Equal(b, data[s.Off:s.Off+int64(s.Len)]) {
			t.Fatalf("chunk at %d carries other bytes", s.Off)
		}
		got = append(got, s)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("stream cut %d chunks, bytes cut %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("chunk %d: stream %+v, bytes %+v", i, got[i], want[i])
		}
	}
	for _, s := range want[:len(want)-1] {
		if s.Len < minChunk || s.Len > maxChunk {
			t.Fatalf("chunk of %d bytes is outside [%d, %d]", s.Len, minChunk, maxChunk)
		}
	}
}

// What makes an old install useful: bytes inserted near the start of a file
// move the chunk boundaries after them with the content, so almost every chunk
// is found again.
func TestInsertionKeepsLaterChunks(t *testing.T) {
	old := noise(2, 2_000_000)
	edited := append(append(append([]byte{}, old[:1000]...), []byte("a few inserted bytes")...), old[1000:]...)
	have := map[string]bool{}
	for _, s := range Split(old) {
		have[HashOf(old[s.Off:s.Off+int64(s.Len)])] = true
	}
	var found, total int
	for _, s := range Split(edited) {
		total += s.Len
		if have[HashOf(edited[s.Off:s.Off+int64(s.Len)])] {
			found += s.Len
		}
	}
	if float64(found)/float64(total) < 0.95 {
		t.Fatalf("only %d of %d bytes found again after a small insertion", found, total)
	}
}

func TestIndexRefusesATreeItCannotApplySafely(t *testing.T) {
	good := File{Path: "a/b.txt", Size: 3, SHA256: HashOf([]byte("abc")), Chunks: []Chunk{{Hash: HashOf([]byte("abc")), Size: 3}}}
	for _, p := range []string{"../escape", "/abs", `a\b`, "C:/x", "a/../b", "a//b", "./a", ""} {
		bad := good
		bad.Path = p
		x := Index{SchemaVersion: SchemaVersion, Version: "v1", Platform: "windows-amd64", Files: []File{bad}}
		if err := x.Validate(); !errors.Is(err, ErrInvalidIndex) {
			t.Fatalf("path %q accepted", p)
		}
	}
	short := good
	short.Size = 4
	if err := (Index{SchemaVersion: SchemaVersion, Version: "v1", Platform: "p", Files: []File{short}}).Validate(); !errors.Is(err, ErrInvalidIndex) {
		t.Fatal("chunks that do not add up to their file were accepted")
	}
	twice := Index{SchemaVersion: SchemaVersion, Version: "v1", Platform: "p", Files: []File{good, {Path: "A/B.txt", Size: good.Size, SHA256: good.SHA256, Chunks: good.Chunks}}}
	if err := twice.Validate(); !errors.Is(err, ErrInvalidIndex) {
		t.Fatal("two paths that are one file on Windows were accepted")
	}
	raw, _ := Index{SchemaVersion: SchemaVersion, Version: "v1", Platform: "p", Files: []File{good}}.Encode()
	if _, err := Decode(raw); err != nil {
		t.Fatalf("an encoded index does not decode: %v", err)
	}
	if _, err := Decode([]byte(strings.Replace(string(raw), `"version"`, `"extra":1,"version"`, 1))); err == nil {
		t.Fatal("an index with an unknown field was accepted")
	}
}

func writeTree(t *testing.T, root string, files map[string][]byte) {
	t.Helper()
	for p, b := range files {
		dst := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// An old install plus the new release's missing chunks rebuilds the new
// release exactly: unchanged files, a file edited in place, one renamed with
// its content, a new one, and one the release dropped.
func TestAnOldInstallAndTheMissingChunksRebuildTheRelease(t *testing.T) {
	exe := noise(3, 1_500_000)
	exe2 := append(append([]byte{}, exe[:700_000]...), append([]byte("version 2.19.0"), exe[700_000:]...)...)
	asset := noise(4, 300_000)
	oldTree := map[string][]byte{"app.exe": exe, "dist/index-AAA.js": asset, "fonts/old.woff2": noise(5, 50_000), "same.dll": noise(6, 400_000)}
	newTree := map[string][]byte{"app.exe": exe2, "dist/index-BBB.js": asset, "fonts/new.woff2": noise(7, 80_000), "same.dll": oldTree["same.dll"]}

	store := map[string][]byte{}
	var entries []Entry
	for p, b := range newTree {
		entries = append(entries, Entry{Path: p, Data: b, Exec: strings.HasSuffix(p, ".exe")})
	}
	x, err := Build("v2", "windows-amd64", entries, func(h string, plain []byte) error {
		store[h] = Compress(plain)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	install := testenv.TempDir(t)
	writeTree(t, install, oldTree)
	p, err := PlanFrom(x, install)
	if err != nil {
		t.Fatal(err)
	}
	if p.MissingBytes > 150_000 {
		t.Fatalf("plan wants %d bytes; the new font and one edited chunk should be all", p.MissingBytes)
	}
	var fetched atomic.Int32
	cache, staging := testenv.TempDir(t), testenv.TempDir(t)
	fetch := func(_ context.Context, h string) ([]byte, error) {
		fetched.Add(1)
		return store[h], nil
	}
	if err := Fetch(t.Context(), p.Missing, fetch, cache, 4, nil); err != nil {
		t.Fatal(err)
	}
	if int(fetched.Load()) != len(p.Missing) {
		t.Fatalf("fetched %d chunks, plan listed %d", fetched.Load(), len(p.Missing))
	}
	if err := Assemble(x, p, install, cache, staging); err != nil {
		t.Fatal(err)
	}
	for path, want := range newTree {
		got, err := os.ReadFile(filepath.Join(staging, filepath.FromSlash(path)))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s was not rebuilt exactly (%v)", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(staging, "fonts", "old.woff2")); !os.IsNotExist(err) {
		t.Fatal("a file the release dropped was staged")
	}
}

// A chunk store that serves the wrong bytes is caught at the chunk, before any
// file is written from it.
func TestAFetchedChunkThatIsNotItsNameIsRefused(t *testing.T) {
	c := Chunk{Hash: HashOf([]byte("expected")), Size: 8}
	fetch := func(context.Context, string) ([]byte, error) { return Compress([]byte("tampered")), nil }
	if err := Fetch(t.Context(), []Chunk{c}, fetch, testenv.TempDir(t), 1, nil); !errors.Is(err, ErrChunkMismatch) {
		t.Fatalf("err = %v, want ErrChunkMismatch", err)
	}
}
