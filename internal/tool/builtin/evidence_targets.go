package builtin

import (
	"context"
	"encoding/json"
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
	return previewEvidence(e.Preview(ctx, args))
}
func (m multiEdit) DeclareEvidenceTarget(ctx context.Context, args json.RawMessage) (tool.EvidenceTargetInfo, error) {
	return previewEvidence(m.Preview(ctx, args))
}
func (d deleteSymbol) DeclareEvidenceTarget(ctx context.Context, args json.RawMessage) (tool.EvidenceTargetInfo, error) {
	return previewEvidence(d.Preview(ctx, args))
}
func (n notebookEdit) DeclareEvidenceTarget(ctx context.Context, args json.RawMessage) (tool.EvidenceTargetInfo, error) {
	return previewEvidence(n.Preview(ctx, args))
}
func (m moveFile) DeclareEvidenceTarget(_ context.Context, _ json.RawMessage) (tool.EvidenceTargetInfo, error) {
	// A move preserves every source byte and refuses an existing destination.
	// Existence/confinement checks belong to Execute; no text is replaced.
	return tool.EvidenceTargetInfo{}, nil
}
