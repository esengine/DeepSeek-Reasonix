package plugin

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// mcpApplicationError is a successful tools/call response whose MCP isError
// flag is set. It is deterministic application feedback, not a transport
// failure, so the agent must never retry it merely because its human-readable
// message contains words such as "timeout" or "unavailable".
type mcpApplicationError struct {
	message string
}

func (e *mcpApplicationError) Error() string { return e.message }

// RetryableToolError is consumed by the agent retry classifier without
// creating a plugin dependency in the tool package.
func (*mcpApplicationError) RetryableToolError() bool { return false }

// Rich MCP content is additive to the ordinary text projection. Bound it
// separately so structured data and embedded resources cannot unexpectedly
// consume an entire model context or smuggle large inline binary payloads into
// provider requests. Existing text blocks retain their historical behavior.
const (
	maxToolResultRichProjectionBytes = 64 << 10
	maxToolResultRichItemBytes       = 32 << 10
	maxToolResultImageBytes          = 4 << 20 // encoded length; stays under provider per-image and request caps
	maxToolResultImages              = 5
)

var toolResultImageMimes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/gif":  true,
	"image/webp": true,
}

// parseToolResult flattens an MCP tools/call result into provider-safe text plus
// image data URLs. Text and direct images preserve their historical projection;
// structured content, resource links, embedded resources, audio, and unknown
// future blocks receive bounded textual projections so they are never silently
// dropped or allowed to inject unbounded inline binary data.
func parseToolResult(res json.RawMessage) (string, []string, error) {
	return parseToolResultProjection(res, true)
}

// parseToolResultForApp keeps the App transcript's historical text/image-only
// projection. The complete bounded result already travels over AppBridge, so
// repeating rich blocks in the local transcript would duplicate content and
// alter an adjacent provider-excluded contract.
func parseToolResultForApp(res json.RawMessage) (string, []string, error) {
	return parseToolResultProjection(res, false)
}

func parseToolResultProjection(res json.RawMessage, includeRich bool) (string, []string, error) {
	var out struct {
		Content           []json.RawMessage `json:"content"`
		StructuredContent json.RawMessage   `json:"structuredContent"`
		IsError           bool              `json:"isError"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return "", nil, fmt.Errorf("decode tool result: %w", err)
	}
	var sb strings.Builder
	var images []string
	projection := newToolResultProjection()
	for _, raw := range out.Content {
		var header struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &header); err != nil {
			return "", nil, fmt.Errorf("decode tool result content: %w", err)
		}
		switch header.Type {
		case "text":
			var content struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(raw, &content); err != nil {
				return "", nil, fmt.Errorf("decode tool result text content: %w", err)
			}
			projection.writeInline(&sb, content.Text)
		case "image":
			var content struct {
				Data     string `json:"data"`
				MimeType string `json:"mimeType"`
			}
			if err := json.Unmarshal(raw, &content); err != nil {
				return "", nil, fmt.Errorf("decode tool result image content: %w", err)
			}
			placeholder, url := toolResultImage(content.MimeType, content.Data, len(images))
			projection.writeInline(&sb, placeholder)
			if url != "" {
				images = append(images, url)
			}
		case "audio":
			if !includeRich {
				continue
			}
			block, err := projectToolResultAudio(raw)
			if err != nil {
				return "", nil, err
			}
			projection.writeBlock(&sb, "audio", block, false)
		case "resource_link":
			if !includeRich {
				continue
			}
			block, err := projectToolResultResourceLink(raw)
			if err != nil {
				return "", nil, err
			}
			projection.writeBlock(&sb, "resource link", block, false)
		case "resource":
			if !includeRich {
				continue
			}
			block, url, err := projectToolResultEmbeddedResource(raw, len(images))
			if err != nil {
				return "", nil, err
			}
			projection.writeBlock(&sb, "embedded resource", block, true)
			if url != "" {
				images = append(images, url)
			}
		default:
			if includeRich {
				projection.writeBlock(&sb, "content block", projectUnknownToolResultContent(header.Type), false)
			}
		}
	}
	if includeRich && hasToolResultStructuredContent(out.StructuredContent) {
		structured := bytes.TrimSpace(out.StructuredContent)
		visible := strings.TrimSpace(sb.String())
		switch {
		case visible == "" && len(structured) <= maxToolResultRichItemBytes:
			canonical, err := canonicalToolResultJSON(structured)
			if err != nil {
				return "", nil, fmt.Errorf("decode tool result structured content: %w", err)
			}
			var compact bytes.Buffer
			if err := json.Compact(&compact, canonical); err != nil {
				return "", nil, fmt.Errorf("compact tool result structured content: %w", err)
			}
			projection.writeInline(&sb, compact.String())
		case visible != "" && toolResultJSONEqual(visible, structured):
			// A text JSON block and structuredContent with the same value are one
			// result, not two independent model observations.
		default:
			block, err := projectToolResultStructuredContent(structured)
			if err != nil {
				return "", nil, err
			}
			projection.writeBlock(&sb, "structured content", block, false)
		}
	}
	text := sb.String()
	if out.IsError {
		return text, images, &mcpApplicationError{message: fmt.Sprintf("plugin tool reported error: %s", text)}
	}
	return text, images, nil
}

// toolResultImage validates one MCP image content item and returns its text
// placeholder plus the data URL to forward ("" when the item is dropped).
func toolResultImage(mime, data string, kept int) (placeholder, url string) {
	if kept >= maxToolResultImages {
		return "[image omitted: per-result image limit reached]", ""
	}
	mime = strings.ToLower(strings.TrimSpace(mime))
	if mime == "" {
		mime = "image/png"
	}
	if !toolResultImageMimes[mime] {
		return "[image omitted: unsupported type " + mime + "]", ""
	}
	// Some servers wrap base64 in whitespace; vision APIs reject non-canonical
	// payloads, so normalize before validating.
	data = normalizeToolResultBase64(data)
	if data == "" {
		return "[image omitted: no data]", ""
	}
	if len(data) > maxToolResultImageBytes {
		return fmt.Sprintf("[image omitted: %d bytes exceeds the %d-byte limit]", len(data), maxToolResultImageBytes), ""
	}
	if _, err := decodedToolResultBase64Bytes(data); err != nil {
		return "[image omitted: invalid base64]", ""
	}
	return "[image: " + mime + "]", "data:" + mime + ";base64," + data
}

type toolResultProjection struct {
	remaining        int
	hasOutput        bool
	endsWithNewline  bool
	separateNextText bool
}

func newToolResultProjection() *toolResultProjection {
	return &toolResultProjection{remaining: maxToolResultRichProjectionBytes}
}

// writeInline preserves the historical concatenation of text and direct image
// placeholders. It adds a separator only after a rich block so the following
// ordinary text cannot be mistaken for resource metadata or JSON.
func (p *toolResultProjection) writeInline(sb *strings.Builder, text string) {
	if p == nil || text == "" {
		return
	}
	if p.separateNextText && p.hasOutput && !p.endsWithNewline {
		sb.WriteByte('\n')
		if p.remaining > 0 {
			p.remaining--
		}
	}
	sb.WriteString(text)
	p.hasOutput = true
	p.endsWithNewline = strings.HasSuffix(text, "\n")
	p.separateNextText = false
}

// writeBlock appends one model-facing rich-content block. Atomic blocks such
// as JSON are replaced with a valid omission marker rather than byte-truncated;
// human-readable embedded text may be clipped at a UTF-8 boundary.
func (p *toolResultProjection) writeBlock(sb *strings.Builder, kind, block string, truncatable bool) {
	if p == nil || block == "" || p.remaining <= 0 {
		return
	}
	if len(block) > maxToolResultRichItemBytes {
		if truncatable {
			block = clipToolResultProjection(block, maxToolResultRichItemBytes)
		} else {
			block = toolResultProjectionOmission(kind, len(block), maxToolResultRichItemBytes, "per-item")
		}
	}

	separatorBytes := 0
	if p.hasOutput && !p.endsWithNewline {
		separatorBytes = 1
	}
	available := p.remaining - separatorBytes
	if available <= 0 {
		return
	}
	if len(block) > available {
		if truncatable {
			block = clipToolResultProjection(block, available)
		} else {
			block = toolResultProjectionOmission(kind, len(block), available, "aggregate")
			if len(block) > available {
				block = clipToolResultProjection(block, available)
			}
		}
	}
	if block == "" {
		return
	}
	if separatorBytes != 0 {
		sb.WriteByte('\n')
		p.remaining--
	}
	sb.WriteString(block)
	p.remaining -= len(block)
	p.hasOutput = true
	p.endsWithNewline = strings.HasSuffix(block, "\n")
	p.separateNextText = true
}

func toolResultProjectionOmission(kind string, size, limit int, scope string) string {
	return fmt.Sprintf("[MCP %s omitted: %d bytes exceed the %d-byte %s projection limit]", kind, size, limit, scope)
}

func clipToolResultProjection(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(text) <= limit {
		return text
	}
	prefixBytes := limit
	for {
		prefix := validToolResultUTF8Prefix(text, prefixBytes)
		suffix := fmt.Sprintf("\n[MCP projection truncated: %d bytes omitted]", len(text)-len(prefix))
		if len(suffix) >= limit {
			return validToolResultUTF8Prefix(suffix, limit)
		}
		allowedPrefix := limit - len(suffix)
		if len(prefix) <= allowedPrefix {
			return prefix + suffix
		}
		prefixBytes = allowedPrefix
	}
}

func validToolResultUTF8Prefix(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(text) <= limit {
		return text
	}
	limit = min(limit, len(text))
	for limit > 0 && !utf8.ValidString(text[:limit]) {
		limit--
	}
	return text[:limit]
}

func hasToolResultStructuredContent(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

func projectToolResultStructuredContent(raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > maxToolResultRichItemBytes {
		return toolResultProjectionOmission("structured content", len(trimmed), maxToolResultRichItemBytes, "per-item"), nil
	}
	canonical, err := canonicalToolResultJSON(trimmed)
	if err != nil {
		return "", fmt.Errorf("decode tool result structured content: %w", err)
	}
	return "[MCP structured content]\n" + string(canonical), nil
}

func canonicalToolResultJSON(raw json.RawMessage) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("multiple JSON values")
		}
		return nil, err
	}
	return json.MarshalIndent(value, "", "  ")
}

func toolResultJSONEqual(text string, structured json.RawMessage) bool {
	left, err := canonicalToolResultJSON(json.RawMessage(text))
	if err != nil {
		return false
	}
	right, err := canonicalToolResultJSON(structured)
	return err == nil && bytes.Equal(left, right)
}

func projectToolResultResourceLink(raw json.RawMessage) (string, error) {
	var content struct {
		URI         string      `json:"uri"`
		Name        string      `json:"name"`
		Title       string      `json:"title"`
		Description string      `json:"description"`
		MimeType    string      `json:"mimeType"`
		Size        json.Number `json:"size"`
	}
	if err := json.Unmarshal(raw, &content); err != nil {
		return "", fmt.Errorf("decode tool result resource link: %w", err)
	}
	metadata := struct {
		URI         string      `json:"uri"`
		Name        string      `json:"name,omitempty"`
		Title       string      `json:"title,omitempty"`
		Description string      `json:"description,omitempty"`
		MimeType    string      `json:"mimeType,omitempty"`
		Size        json.Number `json:"size,omitempty"`
	}{content.URI, content.Name, content.Title, content.Description, content.MimeType, content.Size}
	b, err := json.Marshal(metadata)
	if err != nil {
		return "", fmt.Errorf("encode tool result resource link: %w", err)
	}
	return "[MCP resource link] " + string(b), nil
}

func projectToolResultEmbeddedResource(raw json.RawMessage, keptImages int) (block, imageURL string, err error) {
	var content struct {
		Resource struct {
			URI      string `json:"uri"`
			MimeType string `json:"mimeType"`
			Text     string `json:"text"`
			Blob     string `json:"blob"`
		} `json:"resource"`
	}
	if err := json.Unmarshal(raw, &content); err != nil {
		return "", "", fmt.Errorf("decode tool result embedded resource: %w", err)
	}
	metadata := struct {
		URI      string `json:"uri"`
		MimeType string `json:"mimeType,omitempty"`
	}{content.Resource.URI, content.Resource.MimeType}
	b, err := json.Marshal(metadata)
	if err != nil {
		return "", "", fmt.Errorf("encode tool result embedded resource: %w", err)
	}
	header := "[MCP embedded resource] " + string(b)
	if content.Resource.Text != "" {
		maxTextBytes := maxToolResultRichItemBytes - len(header) - 1
		text := clipToolResultProjection(content.Resource.Text, maxTextBytes)
		return header + "\n" + text, "", nil
	}
	if content.Resource.Blob == "" {
		return header + "\n[MCP embedded resource content omitted: no text or blob]", "", nil
	}
	mime := strings.ToLower(strings.TrimSpace(content.Resource.MimeType))
	if strings.HasPrefix(mime, "image/") {
		placeholder, imageURL := toolResultImage(mime, content.Resource.Blob, keptImages)
		return header + "\n" + placeholder, imageURL, nil
	}
	return header + "\n" + projectToolResultBinary("binary resource", mime, content.Resource.Blob, "omitted"), "", nil
}

func projectToolResultAudio(raw json.RawMessage) (string, error) {
	var content struct {
		Data     string `json:"data"`
		MimeType string `json:"mimeType"`
	}
	if err := json.Unmarshal(raw, &content); err != nil {
		return "", fmt.Errorf("decode tool result audio content: %w", err)
	}
	return projectToolResultBinary("audio", content.MimeType, content.Data, "omitted: no audio provider channel"), nil
}

func projectToolResultBinary(kind, mime, data, validStatus string) string {
	mime = strings.ToLower(strings.TrimSpace(mime))
	normalized := normalizeToolResultBase64(data)
	summary := struct {
		MimeType     string `json:"mimeType,omitempty"`
		EncodedBytes int    `json:"encodedBytes"`
		DecodedBytes *int64 `json:"decodedBytes,omitempty"`
		Data         string `json:"data"`
	}{MimeType: mime, EncodedBytes: len(normalized), Data: validStatus}
	switch {
	case normalized == "":
		summary.Data = "omitted: no data"
	case len(normalized) > maxToolResultImageBytes:
		summary.Data = fmt.Sprintf("omitted: exceeds %d-byte inline base64 limit", maxToolResultImageBytes)
	default:
		decodedBytes, err := decodedToolResultBase64Bytes(normalized)
		if err != nil {
			summary.Data = "omitted: invalid base64"
		} else {
			summary.DecodedBytes = &decodedBytes
		}
	}
	b, err := json.Marshal(summary)
	if err != nil {
		return fmt.Sprintf("[MCP %s omitted: metadata encoding failed]", kind)
	}
	return "[MCP " + kind + "] " + string(b)
}

func normalizeToolResultBase64(data string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '\t', ' ':
			return -1
		}
		return r
	}, data)
}

func decodedToolResultBase64Bytes(data string) (int64, error) {
	return io.Copy(io.Discard, base64.NewDecoder(base64.StdEncoding, strings.NewReader(data)))
}

func projectUnknownToolResultContent(contentType string) string {
	if strings.TrimSpace(contentType) == "" {
		contentType = "missing"
	}
	b, _ := json.Marshal(struct {
		Type string `json:"type"`
	}{contentType})
	return "[unsupported MCP content block] " + string(b)
}
