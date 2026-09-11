package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	udiff "github.com/aymanbagabas/go-udiff"
	"reasonix/internal/diff"
	"reasonix/internal/readcoord"
	"reasonix/internal/tool"
)

// Evidence is resolved by the same preview implementation that validates the
// writer's matches and builds its final edit. Multiple edits are compared to
// the original source, so text created by an earlier step needs no prior read.
func previewEvidence(change diff.Change, err error) (tool.EvidenceTargetInfo, error) {
	if err != nil {
		return tool.EvidenceTargetInfo{}, err
	}
	info := tool.EvidenceTargetInfo{Path: change.Path, SourceTextDigest: digestText(change.OldText)}
	if change.Kind == diff.Create || change.OldText == change.NewText {
		return info, nil
	}
	if change.Binary {
		info.WholeFile = true
		return info, nil
	}
	lines := strings.Split(strings.TrimSuffix(strings.ReplaceAll(change.OldText, "\r\n", "\n"), "\n"), "\n")
	if change.OldText == "" {
		return info, nil
	}
	var ranges []tool.ReadRange
	for _, edit := range udiff.Lines(change.OldText, change.NewText) {
		start := strings.Count(change.OldText[:edit.Start], "\n")
		end := strings.Count(change.OldText[:edit.End], "\n")
		if edit.End > edit.Start && change.OldText[edit.End-1] != '\n' {
			end++
		}
		if start == end {
			start = max(0, start-1)
			end++
		}
		ranges = append(ranges, tool.ReadRange{Start: min(start, len(lines)-1), End: min(end, len(lines))})
	}
	info.Ranges = readcoord.Normalize(ranges)
	for _, r := range info.Ranges {
		for _, line := range lines[r.Start:r.End] {
			info.Hashes = append(info.Hashes, digestText(line))
		}
	}
	return info, nil
}

func (e editFile) DeclareEvidenceTarget(ctx context.Context, args json.RawMessage) (tool.EvidenceTargetInfo, error) {
	change, err := e.Preview(ctx, args)
	return versionedPreviewEvidence(ctx, e.overlay, change, err)
}
func (m multiEdit) DeclareEvidenceTarget(ctx context.Context, args json.RawMessage) (tool.EvidenceTargetInfo, error) {
	change, err := m.Preview(ctx, args)
	return versionedPreviewEvidence(ctx, m.overlay, change, err)
}
func (d deleteSymbol) DeclareEvidenceTarget(ctx context.Context, args json.RawMessage) (tool.EvidenceTargetInfo, error) {
	change, err := d.Preview(ctx, args)
	return versionedPreviewEvidence(ctx, d.overlay, change, err)
}
func (n notebookEdit) DeclareEvidenceTarget(ctx context.Context, args json.RawMessage) (tool.EvidenceTargetInfo, error) {
	change, err := n.Preview(ctx, args)
	return versionedPreviewEvidence(ctx, n.overlay, change, err)
}

// Bind the preview to the same current source route as Execute. A change
// between preview and capture fails closed instead of attaching a new version
// to old hashes; Execute rechecks this identity once more before writing.
func versionedPreviewEvidence(ctx context.Context, overlay FileOverlay, change diff.Change, previewErr error) (tool.EvidenceTargetInfo, error) {
	info, err := previewEvidence(change, previewErr)
	if err != nil || change.Kind == diff.Create {
		return info, err
	}
	src, err := readEditSource(ctx, overlay, change.Path)
	if err != nil {
		return tool.EvidenceTargetInfo{}, err
	}
	if src.content != change.OldText {
		return tool.EvidenceTargetInfo{}, &tool.OperationError{Diagnostic: tool.OperationDiagnostic{Code: tool.WriteEvidenceStale, Path: change.Path, Recovery: "source changed during evidence preview; re-read then retry"}, Cause: ErrFileChanged}
	}
	if err := src.assertUnchanged(ctx, overlay, change.Path); err != nil {
		return tool.EvidenceTargetInfo{}, err
	}
	info.Snapshot = src.readSnapshot(change.Path)
	return info, nil
}
func (m moveFile) DeclareEvidenceTarget(ctx context.Context, args json.RawMessage) (tool.EvidenceTargetInfo, error) {
	var p struct {
		Source      string `json:"source_path"`
		Destination string `json:"destination_path"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return tool.EvidenceTargetInfo{}, err
	}
	if p.Source == "" || p.Destination == "" {
		return tool.EvidenceTargetInfo{}, fmt.Errorf("source_path and destination_path are required")
	}
	source, dest := resolveIn(m.workDir, p.Source), resolveIn(m.workDir, p.Destination)
	if source == dest {
		return tool.EvidenceTargetInfo{}, nil
	}
	for _, path := range []string{source, dest} {
		if err := confinePreview(effectiveWriteRoots(ctx, m.rootSet, m.roots), m.guard, m.managed, path); err != nil {
			return tool.EvidenceTargetInfo{}, err
		}
	}
	id, err := diskIdentity(source)
	if err != nil {
		return tool.EvidenceTargetInfo{}, err
	}
	if !id.existed {
		return tool.EvidenceTargetInfo{}, &tool.OperationError{Diagnostic: tool.OperationDiagnostic{Code: tool.WriteTargetAbsent, Path: source, Recovery: "read the source at its current path"}, Cause: os.ErrNotExist}
	}
	return tool.EvidenceTargetInfo{Path: source, Snapshot: tool.SourceSnapshot(tool.ReadSourceDisk, source, fmt.Sprintf("raw-sha256:%x", id.sum)), PreservesContent: true}, nil
}
