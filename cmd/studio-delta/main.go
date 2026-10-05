// Command studio-delta turns a Studio release archive into a delta index and
// the chunks it is made of (build), and replays an update from an unpacked
// older release against them (simulate) to measure and check it before any
// client does.
package main

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"reasonix/internal/platform/delta"
	"reasonix/internal/platform/update"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "studio-delta:", err)
		os.Exit(1)
	}
}

const usage = `usage:
  studio-delta build <archive.zip> <version> <platform> <delta-dir>
  studio-delta simulate <delta-dir> <platform> <install-root> <staging-dir>`

func run(args []string) error {
	switch {
	case len(args) == 5 && args[0] == "build":
		return build(args[1], args[2], args[3], args[4])
	case len(args) == 5 && args[0] == "simulate":
		return simulate(args[1], args[2], args[3], args[4])
	default:
		return errors.New(usage)
	}
}

// build writes <delta-dir>/<platform>/index.json.zst and every chunk to
// <delta-dir>/chunks, the store all platforms and releases share.
func build(archive, version, platform, outdir string) error {
	entries, err := readArchive(archive)
	if err != nil {
		return err
	}
	chunks := filepath.Join(outdir, "chunks")
	if err := os.MkdirAll(chunks, 0o755); err != nil {
		return err
	}
	var n int
	var stored int64
	x, err := delta.Build(version, platform, entries, func(h string, plain []byte) error {
		z := delta.Compress(plain)
		n++
		stored += int64(len(z))
		return os.WriteFile(filepath.Join(chunks, delta.ObjectName(h)), z, 0o644)
	})
	if err != nil {
		return err
	}
	raw, err := x.Encode()
	if err != nil {
		return err
	}
	dir := filepath.Join(outdir, platform)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	packed, err := delta.PackIndex(raw)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, update.DeltaIndexName), packed, 0o644); err != nil {
		return err
	}
	fmt.Printf("%s %s: %d files, %d chunks, %.1f MB stored, index %.1f KB (%.1f KB packed)\n",
		version, platform, len(x.Files), n, float64(stored)/1e6, float64(len(raw))/1e3, float64(len(packed))/1e3)
	return nil
}

// readArchive reads every regular file of a release zip. A zip whose entries
// all sit under one top directory is read from inside it, which is how the
// tree looks once installed.
func readArchive(archive string) ([]delta.Entry, error) {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var entries []delta.Entry
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		entries = append(entries, delta.Entry{Path: path.Clean(f.Name), Data: b, Exec: f.Mode()&0o111 != 0})
	}
	return stripTop(entries), nil
}

func stripTop(entries []delta.Entry) []delta.Entry {
	if len(entries) == 0 {
		return entries
	}
	top, _, ok := strings.Cut(entries[0].Path, "/")
	if !ok {
		return entries
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Path, top+"/") {
			return entries
		}
	}
	for i := range entries {
		entries[i].Path = strings.TrimPrefix(entries[i].Path, top+"/")
	}
	return entries
}

// simulate updates install-root to the release in outdir, fetching chunks from
// outdir's store as a client would from the network, and reports the cost.
func simulate(outdir, platform, install, staging string) error {
	packed, err := os.ReadFile(filepath.Join(outdir, platform, update.DeltaIndexName))
	if err != nil {
		return err
	}
	x, err := delta.UnpackIndex(packed)
	if err != nil {
		return err
	}
	p, err := delta.PlanFrom(x, install)
	if err != nil {
		return err
	}
	var wire int64
	fetch := func(_ context.Context, h string) ([]byte, error) {
		b, err := os.ReadFile(filepath.Join(outdir, "chunks", delta.ObjectName(h)))
		wire += int64(len(b))
		return b, err
	}
	cache := staging + ".chunks"
	if err := delta.Fetch(context.Background(), p.Missing, fetch, cache, 1, nil); err != nil {
		return err
	}
	if err := delta.Assemble(x, p, install, cache, staging); err != nil {
		return err
	}
	var total int64
	for _, f := range x.Files {
		total += f.Size
	}
	fmt.Printf("to %s: %d missing chunks, %.1f MB plain, %.1f MB downloaded; %.1f MB of %.1f MB reused; staged tree verified\n",
		x.Version, len(p.Missing), float64(p.MissingBytes)/1e6, float64(wire)/1e6, float64(p.ReuseBytes)/1e6, float64(total)/1e6)
	return nil
}
