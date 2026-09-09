package agent

import (
	"encoding/json"
	"strconv"
	"strings"

	"reasonix/internal/tool"
)

type readFileArgs struct {
	Path           string
	Intent         string
	Offset         int
	Limit          int
	OffsetExplicit bool
	LimitExplicit  bool
}

// fullRead reports whether the call promised whole-file coverage. Only an
// explicit full intent does: a default read is a bounded preview and creates no
// outstanding debt just because content remains.
func (a readFileArgs) fullRead() bool { return a.Intent == string(tool.ReadIntentFull) }

type readFileTrailer struct {
	nextOffset   int
	requestedEnd int
	hasMore      bool
	localSafety  bool
}

type sessionToolResultPageHeader struct {
	ResultRef  string `json:"result_ref"`
	Offset     int    `json:"offset"`
	NextOffset int    `json:"next_offset"`
	TotalBytes int    `json:"total_bytes"`
	SHA256     string `json:"sha256"`
	Complete   bool   `json:"complete"`
}

func parseReadFileArgs(args json.RawMessage) (readFileArgs, bool) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil {
		return readFileArgs{}, false
	}
	var out readFileArgs
	if raw, ok := fields["path"]; ok {
		_ = json.Unmarshal(raw, &out.Path)
	}
	if strings.TrimSpace(out.Path) == "" {
		return readFileArgs{}, false
	}
	if raw, ok := fields["intent"]; ok {
		_ = json.Unmarshal(raw, &out.Intent)
	}
	if raw, ok := fields["offset"]; ok {
		out.OffsetExplicit = true
		_ = json.Unmarshal(raw, &out.Offset)
	}
	if raw, ok := fields["limit"]; ok {
		out.LimitExplicit = true
		_ = json.Unmarshal(raw, &out.Limit)
	}
	return out, true
}

func parseReadFileTrailer(output string) readFileTrailer {
	const safetyPrefix = "\n[read_file local safety page; next_offset="
	if start := strings.LastIndex(output, safetyPrefix); start >= 0 && strings.HasSuffix(output, "]\n") {
		fields := strings.TrimSuffix(output[start+len(safetyPrefix):], "]\n")
		parts := strings.Fields(fields)
		if len(parts) == 2 {
			next, nextErr := strconv.Atoi(parts[0])
			endText := strings.TrimPrefix(parts[1], "requested_end=")
			end, endErr := strconv.Atoi(endText)
			if nextErr == nil && endErr == nil && next >= 0 && end >= next {
				return readFileTrailer{nextOffset: next, requestedEnd: end, hasMore: true, localSafety: true}
			}
		}
	}
	const prefix = "\n[more lines below; pass offset="
	start := strings.LastIndex(output, prefix)
	if start < 0 || !strings.HasSuffix(output, "]\n") {
		return readFileTrailer{}
	}
	value := output[start+len(prefix):]
	if end := strings.IndexAny(value, " ]\r\n"); end >= 0 {
		value = value[:end]
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		return readFileTrailer{}
	}
	return readFileTrailer{nextOffset: n, hasMore: true}
}

func readFileRecoveryOffset(output string) (int, bool) {
	const marker = "\n\n…[truncated tool=read_file "
	const field = " next_offset="
	markerStart := strings.LastIndex(output, marker)
	if markerStart < 0 {
		return 0, false
	}
	start := strings.Index(output[markerStart:], field)
	if start < 0 {
		return 0, false
	}
	start += markerStart
	value := output[start+len(field):]
	if end := strings.IndexAny(value, " ]\r\n"); end >= 0 {
		value = value[:end]
	}
	n, err := strconv.Atoi(value)
	return n, err == nil && n >= 0
}

func parseSessionToolResultPage(output string) (sessionToolResultPageHeader, string, bool) {
	headerText, body, ok := strings.Cut(output, "\n")
	if !ok {
		return sessionToolResultPageHeader{}, "", false
	}
	var header sessionToolResultPageHeader
	if err := json.Unmarshal([]byte(headerText), &header); err != nil {
		return sessionToolResultPageHeader{}, "", false
	}
	return header, body, true
}
