package serve

import (
	"net/http"
	"sort"

	"reasonix/internal/checkpoint"
)

type serveCheckpointMeta struct {
	Turn               int      `json:"turn"`
	Prompt             string   `json:"prompt"`
	Files              []string `json:"files"`
	FileCount          int      `json:"fileCount"`
	FilesTruncated     bool     `json:"filesTruncated,omitempty"`
	TurnFileCount      int      `json:"turnFileCount"`
	Time               int64    `json:"time"`
	CanCode            bool     `json:"canCode"`
	CanConversation    bool     `json:"canConversation"`
	Coverage           string   `json:"coverage,omitempty"`
	CoverageGaps       []string `json:"coverageGaps,omitempty"`
	ExpiredFilePayload bool     `json:"expiredFilePayload,omitempty"`
	ActiveWriters      int      `json:"activeWriters,omitempty"`
	Legacy             bool     `json:"legacy,omitempty"`
	CanUndoFiles       bool     `json:"canUndoFiles,omitempty"`
	DisabledReason     string   `json:"disabledReason,omitempty"`
}

const serveCheckpointFilePreviewLimit = 60

func serveCheckpointMetas(raw []checkpoint.Meta, hasBoundary func(int) bool) []serveCheckpointMeta {
	out := make([]serveCheckpointMeta, 0, len(raw))
	for _, item := range raw {
		gaps := make([]string, 0, len(item.CoverageGaps))
		for _, gap := range item.CoverageGaps {
			if gap.Detail != "" {
				gaps = append(gaps, gap.Reason+": "+gap.Detail)
			} else {
				gaps = append(gaps, gap.Reason)
			}
		}
		out = append(out, serveCheckpointMeta{
			Turn: item.Turn, Prompt: item.Prompt, Files: append([]string{}, item.Paths...),
			TurnFileCount: len(item.Paths), Time: item.Time.UnixMilli(),
			CanCode: len(item.Paths) > 0 && item.CanUndoFiles, CanConversation: hasBoundary(item.Turn),
			Coverage: string(item.Coverage), CoverageGaps: gaps, ExpiredFilePayload: item.ExpiredFilePayload,
			ActiveWriters: len(item.ActiveWriters), Legacy: item.Legacy, CanUndoFiles: item.CanUndoFiles,
			DisabledReason: item.DisabledReason,
		})
	}

	hasCodeAfter, canCodeAfter := false, true
	codeFiles, preview := make(map[string]bool, len(raw)*2), []string{}
	//nolint:modernize // the body writes through the index while walking backwards.
	for i := len(out) - 1; i >= 0; i-- {
		if len(out[i].Files) > 0 {
			hasCodeAfter = true
			if !out[i].CanUndoFiles {
				canCodeAfter = false
			}
		}
		for _, path := range out[i].Files {
			if codeFiles[path] {
				continue
			}
			codeFiles[path] = true
			idx := sort.SearchStrings(preview, path)
			if len(preview) < serveCheckpointFilePreviewLimit {
				preview = append(preview, "")
				copy(preview[idx+1:], preview[idx:])
				preview[idx] = path
			} else if idx < serveCheckpointFilePreviewLimit {
				copy(preview[idx+1:], preview[idx:serveCheckpointFilePreviewLimit-1])
				preview[idx] = path
			}
		}
		out[i].CanCode, out[i].FileCount = hasCodeAfter && canCodeAfter, len(codeFiles)
		out[i].Files = append([]string{}, preview...)
		out[i].FilesTruncated = out[i].FileCount > len(out[i].Files)
	}
	return out
}

// checkpoints returns complete rewind capabilities and path previews. File
// contents never cross the Serve boundary.
func (s *Server) checkpoints(w http.ResponseWriter, _ *http.Request) {
	ctrl := s.ctl()
	writeJSON(w, serveCheckpointMetas(ctrl.Checkpoints(), ctrl.CheckpointHasBoundary))
}
