package main

import (
	"fmt"
	"os"
	"path/filepath"

	"reasonix/internal/platform/update"
)

// deltas lists each platform's chunked update under deltaDir, laid out by
// studio-delta as <platform>/index.json.zst beside its signature. They are
// served from the mirror, never GitHub: the chunks they name exist only there.
func deltas(deltaDir, tag string) (map[string]update.Delta, error) {
	entries, err := os.ReadDir(deltaDir)
	if err != nil {
		return nil, err
	}
	out := map[string]update.Delta{}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "chunks" {
			continue
		}
		platform := e.Name()
		index := filepath.Join(deltaDir, platform, update.DeltaIndexName)
		if _, err := os.Stat(index + ".minisig"); err != nil {
			return nil, fmt.Errorf("studio-manifest: %s delta index is not signed: %w", platform, err)
		}
		size, sum, err := hashFile(index)
		if err != nil {
			return nil, err
		}
		url := fmt.Sprintf("%s/%s/delta/%s/%s", update.StudioMirror, tag, platform, update.DeltaIndexName)
		out[platform] = update.Delta{
			Index:  update.Asset{URL: url, Sig: url + ".minisig", Size: size, SHA256: sum},
			Chunks: update.StudioChunks,
		}
		fmt.Printf("delta: %s (%d bytes index)\n", platform, size)
	}
	return out, nil
}
