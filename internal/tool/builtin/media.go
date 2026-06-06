package builtin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/tool"
)

func init() { tool.RegisterBuiltin(mediaTool{}) }

type mediaTool struct{}

const (
	mediaTimeout     = 180 * time.Second // video/audio may need longer
	mediaMaxFileSize = 50 * 1024 * 1024  // 50 MB (matching MiMo base64 limit)
	// Default via model used for multimodal analysis (mimo-v2.5 Omni)
	defaultMediaModel = "mimo-v2.5"
)

func (mediaTool) Name() string { return "media" }

func (mediaTool) Description() string {
	return "Analyze an image, audio, or video file using MiMo's multimodal model. Accepts a local file path and an optional prompt. Returns the model's analysis as text. Supports images (PNG/JPEG/GIF/WebP/BMP), audio (MP3/WAV/FLAC/M4A/OGG), and video (MP4/MOV/AVI/WMV). Requires MIMO_API_KEY in environment."
}

func (mediaTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "path":{"type":"string","description":"Local file path to the image/audio/video file."},
  "prompt":{"type":"string","description":"Optional analysis prompt. Default: '请详细描述这个媒体文件的内容'"},
  "media_type":{"type":"string","enum":["image","audio","video"],"description":"Optional media type hint. Auto-detected from file extension when omitted."}
},
"required":["path"]
}`)
}

func (mediaTool) ReadOnly() bool { return false }

// mediaArgs mirrors the tool's JSON Schema.
type mediaArgs struct {
	Path      string `json:"path"`
	Prompt    string `json:"prompt"`
	MediaType string `json:"media_type"`
}

// mimoRequest is the wire format for MiMo's chat completions API.
type mimoRequest struct {
	Model    string            `json:"model"`
	Messages []mimoMessage     `json:"messages"`
	MaxTokens int              `json:"max_completion_tokens,omitempty"`
	Stream   bool              `json:"stream,omitempty"`
}

type mimoMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// mimoResponse is the top-level API response.
type mimoResponse struct {
	Choices []mimoChoice `json:"choices"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type mimoChoice struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
}

func (mediaTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p mediaArgs
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Path == "" {
		return "", fmt.Errorf("path is required")
	}
	prompt := strings.TrimSpace(p.Prompt)
	if prompt == "" {
		prompt = "请详细描述这个媒体文件的内容"
	}

	// --- read file ---
	info, err := os.Stat(p.Path)
	if err != nil {
		return "", fmt.Errorf("media: stat %s: %w", p.Path, err)
	}
	if info.Size() > mediaMaxFileSize {
		return "", fmt.Errorf("media: file %s is %d bytes (max %d)", p.Path, info.Size(), mediaMaxFileSize)
	}
	raw, err := os.ReadFile(p.Path)
	if err != nil {
		return "", fmt.Errorf("media: read %s: %w", p.Path, err)
	}

	// --- detect MIME type ---
	mimeType := detectMIME(raw, p.Path)
	if mimeType == "" {
		return "", fmt.Errorf("media: cannot detect MIME type for %s", p.Path)
	}

	// --- detect media type ---
	mediaType := strings.ToLower(strings.TrimSpace(p.MediaType))
	if mediaType == "" {
		mediaType = detectMediaType(mimeType)
	}
	if mediaType == "" {
		return "", fmt.Errorf("media: unsupported file type for %s (MIME: %s)", p.Path, mimeType)
	}
	if !supportedMediaType(mediaType, mimeType) {
		return "", fmt.Errorf("media: %s MIME %s is not supported for %s mode", p.Path, mimeType, mediaType)
	}

	// --- base64 encode ---
	b64 := base64.StdEncoding.EncodeToString(raw)

	// --- build content array ---
	parts := buildContentParts(mediaType, b64, mimeType, prompt)
	contentJSON, err := json.Marshal(parts)
	if err != nil {
		return "", fmt.Errorf("media: marshal content: %w", err)
	}

	// --- build request ---
	apiKey := os.Getenv("MIMO_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("media: MIMO_API_KEY is not set")
	}

	body := mimoRequest{
		Model:    defaultMediaModel,
		MaxTokens: 4096,
		Messages: []mimoMessage{
			{Role: "user", Content: contentJSON},
		},
	}
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("media: marshal request: %w", err)
	}

	// --- send ---
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.xiaomimimo.com/v1/chat/completions",
		bytes.NewReader(bodyJSON))
	if err != nil {
		return "", fmt.Errorf("media: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("api-key", apiKey)

	client := &http.Client{Timeout: mediaTimeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("media: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MB cap
	if err != nil {
		return "", fmt.Errorf("media: read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("media: API returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var apiResp mimoResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return "", fmt.Errorf("media: decode response: %w", err)
	}
	if apiResp.Error != nil {
		return "", fmt.Errorf("media: API error: %s", apiResp.Error.Message)
	}
	if len(apiResp.Choices) == 0 {
		return "", fmt.Errorf("media: API returned no choices")
	}

	return strings.TrimSpace(apiResp.Choices[0].Message.Content), nil
}

// --- helpers ---

// buildContentParts constructs the content array for MiMo's multimodal API.
func buildContentParts(mediaType, b64, mimeType, prompt string) []json.RawMessage {
	var dataURI string
	switch mediaType {
	case "image", "video":
		dataURI = fmt.Sprintf("data:%s;base64,%s", mimeType, b64)
	}

	var parts []json.RawMessage
	switch mediaType {
	case "image":
		parts = append(parts, json.RawMessage(
			fmt.Sprintf(`{"type":"image_url","image_url":{"url":"%s"}}`, dataURI)))
	case "audio":
		// audio uses input_audio with data field (not url)
		parts = append(parts, json.RawMessage(
			fmt.Sprintf(`{"type":"input_audio","input_audio":{"data":"data:%s;base64,%s"}}`, mimeType, b64)))
	case "video":
		parts = append(parts, json.RawMessage(
			fmt.Sprintf(`{"type":"video_url","video_url":{"url":"%s"},"fps":2,"media_resolution":"default"}`, dataURI)))
	}
	if prompt != "" {
		parts = append(parts, json.RawMessage(
			fmt.Sprintf(`{"type":"text","text":%s}`, jsonQuote(prompt))))
	}
	return parts
}

// jsonQuote returns s as a JSON-escaped string literal.
func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// detectMIME returns the MIME type for media content, using http.DetectContentType
// and falling back to extension-based detection when content sniffing yields a
// non-media type (e.g. "application/octet-stream" or "text/plain" for OGG/FLAC).
func detectMIME(data []byte, path string) string {
	mime := http.DetectContentType(data[:min(len(data), 512)])
	// Strip charset suffix (e.g. "text/plain; charset=utf-8" → "text/plain").
	if semi := strings.IndexByte(mime, ';'); semi >= 0 {
		mime = strings.TrimSpace(mime[:semi])
	}
	// Normalise: http.DetectContentType returns "audio/wave" for WAV.
	if mime == "audio/wave" {
		mime = "audio/wav"
	}
	// If content sniffing produced a known media type, use it.
	if strings.HasPrefix(mime, "image/") || strings.HasPrefix(mime, "audio/") || strings.HasPrefix(mime, "video/") {
		return mime
	}
	// Fall back to extension-based detection (catches OGG, FLAC, AVI, etc.).
	return mimeByExt(path)
}

// mimeByExt maps known media extensions to MIME types.
func mimeByExt(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".flac":
		return "audio/flac"
	case ".m4a":
		return "audio/mp4"
	case ".ogg":
		return "audio/ogg"
	case ".mp4":
		return "video/mp4"
	case ".mov":
		return "video/quicktime"
	case ".avi":
		return "video/x-msvideo"
	case ".wmv":
		return "video/x-ms-wmv"
	}
	return ""
}

// detectMediaType maps a MIME type to "image", "audio", or "video".
func detectMediaType(mime string) string {
	mime = strings.ToLower(mime)
	switch {
	case strings.HasPrefix(mime, "image/"):
		return "image"
	case strings.HasPrefix(mime, "audio/"):
		return "audio"
	case strings.HasPrefix(mime, "video/"):
		return "video"
	}
	return ""
}

// supportedMediaType validates that the media type + MIME are a known supported combo.
func supportedMediaType(mediaType, mime string) bool {
	switch mediaType {
	case "image":
		switch mime {
		case "image/png", "image/jpeg", "image/gif", "image/webp", "image/bmp":
			return true
		}
	case "audio":
		switch mime {
		case "audio/mpeg", "audio/wav", "audio/flac", "audio/mp4", "audio/ogg":
			return true
		}
	case "video":
		switch mime {
		case "video/mp4", "video/quicktime", "video/x-msvideo", "video/x-ms-wmv":
			return true
		}
	}
	return false
}

// min returns the smaller of a and b (compatible with Go <1.21).
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
