package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/contract/provider"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/store"
)

// A fold moves the earlier conversation into a file and leaves a pointer; the
// model that continues knows what it still needs, so it reads the archive
// itself instead of inheriting a digest.

// outputsDir is where content moved out of context is parked: beside the
// transcript when there is one, the archive the agent inherited otherwise, and
// failing both the scratch directory. read_file reaches all three.
func (a *contextWindow) outputsDir() string {
	if dir := store.SessionOutputsDir(a.sessionPath()); dir != "" {
		return dir
	}
	if root := strings.TrimSpace(a.archiveDir); root != "" {
		return filepath.Join(root, "outputs")
	}
	return sessionstore.ScratchOutputsDir()
}

// foldToArchive writes the fold verbatim and returns the pointer that stands
// in for it. The file is named by its content, so folding the same span twice
// finds the same file.
func (a *contextWindow) foldToArchive(fold []provider.Message, instructions string) (foldSummary, error) {
	res := foldSummary{Mode: CompactionModeArchived, Spans: 1, FoldTokens: a.summaryInputTokens(fold)}
	body := renderTranscriptVerbatim(fold)
	if strings.TrimSpace(body) == "" {
		return res, fmt.Errorf("archive: nothing to archive")
	}
	sum := sha256.Sum256([]byte(body))
	name := "fold-" + hex.EncodeToString(sum[:6]) + ".md"
	var path string
	var werr error
	for _, dir := range []string{a.outputsDir(), sessionstore.ScratchOutputsDir()} {
		if dir == "" {
			continue
		}
		if werr = os.MkdirAll(dir, 0o755); werr == nil {
			path = filepath.Join(dir, name)
			if werr = os.WriteFile(path, []byte(body), 0o644); werr == nil {
				break
			}
		}
		path = ""
	}
	if path == "" {
		if werr == nil {
			werr = fmt.Errorf("nowhere to write")
		}
		return res, fmt.Errorf("archive: %w", werr)
	}
	res.Text = archivePointer(path, len(fold), instructions)
	return res, nil
}

func archivePointer(path string, n int, instructions string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d earlier message(s) were moved out of this conversation to free context. They were archived, not summarized: the record is complete and verbatim.\n\n", n)
	fmt.Fprintf(&b, "Archive: %s\n\n", path)
	b.WriteString("Read it with read_file (a window at a time when it is long) or search it with grep for a name, path or keyword. Before you continue, recover from it the goal, the decisions made, the files touched and what remains, and say what you will do next. Do not guess what it contains.")
	if focus := strings.TrimSpace(instructions); focus != "" {
		b.WriteString("\n\nWhat the user asked you to keep in focus: " + focus)
	}
	return b.String()
}
