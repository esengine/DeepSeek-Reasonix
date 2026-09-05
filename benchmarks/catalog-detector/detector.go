package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"reasonix/internal/skill"
)

// detector answers one question: has the thing the model would be sent changed?
// Each returns an opaque token — equal tokens mean "not owed". Nothing here
// counts files or bytes: accounting is a second walk, and timing a detector
// with the instrument attached measures the instrument.
type detector struct {
	name  string
	read  func(env) string
	walks bool // whether this detector's cost is a directory walk
}

// env is one workspace under measurement.
type env struct {
	projectRoot string
	homeDir     string
	// apiWrites is the observed-write detector's whole input: whether the last
	// mutation went through a writer this process controls.
	apiWrites *int
}

func (e env) store() *skill.Store {
	return skill.New(skill.Options{
		ProjectRoot: e.projectRoot, ReasonixHomeDir: filepath.Join(e.homeDir, ".reasonix"), Stderr: io.Discard,
	})
}

func (e env) roots() []string {
	var out []string
	for _, r := range e.store().Roots() {
		out = append(out, r.Dir)
	}
	return out
}

// projectionDetector is the truth, not an estimate of it: it renders the block
// the model would receive and compares that. A change no listing shows — a body
// edit, a touched mtime — is by construction not owed.
var projectionDetector = detector{
	name:  "projection",
	walks: true,
	read: func(e env) string {
		return digest(skill.IndexBlock(e.store().List()))
	},
}

// metadataDetector is the cheap approximation: path, size and mtime, no reads.
var metadataDetector = detector{
	name:  "metadata",
	walks: true,
	read: func(e env) string {
		var lines []string
		for _, dir := range e.roots() {
			_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				info, err := d.Info()
				if err != nil {
					return nil
				}
				lines = append(lines, fmt.Sprintf("%s\x00%d\x00%d", p, info.Size(), info.ModTime().UnixNano()))
				return nil
			})
		}
		sort.Strings(lines)
		return digest(strings.Join(lines, "\n"))
	},
}

// observedWriteDetector sees only what this process was told about. It reads
// nothing, so its cost is zero and its blindness is total: a write it was not
// told about leaves the token where it was.
var observedWriteDetector = detector{
	name: "observed-write",
	read: func(e env) string {
		return fmt.Sprintf("writes=%d", *e.apiWrites)
	},
}

var detectors = []detector{projectionDetector, metadataDetector, observedWriteDetector}

// walkCost reports the shape of what a walking detector touches. It runs
// outside the timed section for exactly the reason this file has two functions
// instead of one.
func walkCost(dirs []string) (files int, bytes int64) {
	for _, dir := range dirs {
		_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			files++
			if info, err := os.Stat(p); err == nil {
				bytes += info.Size()
			}
			return nil
		})
	}
	return files, bytes
}

func digest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:12]
}
