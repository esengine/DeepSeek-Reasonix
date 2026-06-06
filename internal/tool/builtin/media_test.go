package builtin

import (
	"encoding/json"
	"testing"
)

func TestMediaDetectMIME(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		data  []byte
		want  string // expected prefix
	}{
		{"PNG header", "test.png", []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, "image/png"},
		{"JPEG header", "test.jpg", []byte{0xFF, 0xD8, 0xFF, 0xE0}, "image/jpeg"},
		{"JPEG alt ext", "test.jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0}, "image/jpeg"},
		{"GIF header", "test.gif", []byte{0x47, 0x49, 0x46, 0x38, 0x39, 0x61}, "image/gif"},
		{"WebP header", "test.webp", []byte{0x52, 0x49, 0x46, 0x46, 0x00, 0x00, 0x00, 0x00, 0x57, 0x45, 0x42, 0x50, 0x56, 0x50}, "image/webp"},
		{"BMP ext fallback", "test.bmp", []byte{0x00, 0x00, 0x00, 0x00}, "image/bmp"},
		// Audio
		{"MP3 ID3 header", "test.mp3", []byte{0x49, 0x44, 0x33, 0x03, 0x00, 0x00, 0x00}, "audio/mpeg"},
		{"WAV header", "test.wav", []byte{0x52, 0x49, 0x46, 0x46, 0x00, 0x00, 0x00, 0x00, 0x57, 0x41, 0x56, 0x45}, "audio/wav"},
		{"FLAC ext", "test.flac", []byte{0x00, 0x00, 0x00, 0x00}, "audio/flac"},
		{"M4A ext", "test.m4a", []byte{0x00, 0x00, 0x00, 0x00}, "audio/mp4"},
		{"OGG ext", "test.ogg", []byte{0x4F, 0x67, 0x67, 0x53}, "audio/ogg"},
		// Video
		{"MP4 header", "test.mp4", []byte{0x00, 0x00, 0x00, 0x1C, 0x66, 0x74, 0x79, 0x70, 0x6D, 0x70, 0x34, 0x32}, "video/mp4"},
		{"MOV ext", "test.mov", []byte{0x00, 0x00, 0x00, 0x00}, "video/quicktime"},
		{"AVI ext", "test.avi", []byte{0x00, 0x00, 0x00, 0x00}, "video/x-msvideo"},
		{"WMV ext", "test.wmv", []byte{0x00, 0x00, 0x00, 0x00}, "video/x-ms-wmv"},
		// Unsupported (not a media MIME, falls through to ext-based → none returns "")
		{"TXT file", "test.txt", []byte("hello world"), ""},
		{"unknown ext", "test.xyz", []byte{0x00, 0x00, 0x00, 0x00}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectMIME(tt.data, tt.path)
			if got != tt.want {
				t.Errorf("detectMIME(%q, %q) = %q, want %q", tt.data, tt.path, got, tt.want)
			}
		})
	}
}

func TestMediaDetectMediaType(t *testing.T) {
	tests := []struct {
		mime string
		want string
	}{
		{"image/png", "image"},
		{"image/jpeg", "image"},
		{"image/gif", "image"},
		{"audio/mpeg", "audio"},
		{"audio/wav", "audio"},
		{"video/mp4", "video"},
		{"video/quicktime", "video"},
		{"application/pdf", ""},
		{"text/plain", ""},
	}
	for _, tt := range tests {
		t.Run(tt.mime, func(t *testing.T) {
			got := detectMediaType(tt.mime)
			if got != tt.want {
				t.Errorf("detectMediaType(%q) = %q, want %q", tt.mime, got, tt.want)
			}
		})
	}
}

func TestMediaSupportedMediaType(t *testing.T) {
	tests := []struct {
		mediaType string
		mime      string
		want      bool
	}{
		{"image", "image/png", true},
		{"image", "image/jpeg", true},
		{"image", "image/gif", true},
		{"image", "image/webp", true},
		{"image", "image/bmp", true},
		{"image", "image/tiff", false}, // not in MiMo supported list
		{"audio", "audio/mpeg", true},
		{"audio", "audio/wav", true},
		{"audio", "audio/flac", true},
		{"audio", "audio/mp4", true},
		{"audio", "audio/ogg", true},
		{"audio", "audio/aac", false}, // not in MiMo supported list
		{"video", "video/mp4", true},
		{"video", "video/quicktime", true},
		{"video", "video/x-msvideo", true},
		{"video", "video/x-ms-wmv", true},
		{"video", "video/mpeg", false}, // not in MiMo supported list
	}
	for _, tt := range tests {
		t.Run(tt.mime, func(t *testing.T) {
			got := supportedMediaType(tt.mediaType, tt.mime)
			if got != tt.want {
				t.Errorf("supportedMediaType(%q, %q) = %v, want %v", tt.mediaType, tt.mime, got, tt.want)
			}
		})
	}
}

func TestMediaBuildContentParts(t *testing.T) {
	b64 := "dGVzdA==" // base64 of "test"
	mime := "image/png"
	prompt := "describe this"

	parts := buildContentParts("image", b64, mime, prompt)
	if len(parts) != 2 {
		t.Fatalf("buildContentParts image: got %d parts, want 2", len(parts))
	}
	// First part should be image_url
	var first map[string]any
	if err := jsonUnmarshal(parts[0], &first); err != nil {
		t.Fatalf("unmarshal first part: %v", err)
	}
	if first["type"] != "image_url" {
		t.Errorf("first part type = %v, want image_url", first["type"])
	}

	// Audio
	parts = buildContentParts("audio", b64, "audio/wav", prompt)
	if len(parts) != 2 {
		t.Fatalf("buildContentParts audio: got %d parts, want 2", len(parts))
	}
	if err := jsonUnmarshal(parts[0], &first); err != nil {
		t.Fatalf("unmarshal first part: %v", err)
	}
	if first["type"] != "input_audio" {
		t.Errorf("first part type = %v, want input_audio", first["type"])
	}

	// Video
	parts = buildContentParts("video", b64, "video/mp4", prompt)
	if len(parts) != 2 {
		t.Fatalf("buildContentParts video: got %d parts, want 2", len(parts))
	}
	if err := jsonUnmarshal(parts[0], &first); err != nil {
		t.Fatalf("unmarshal first part: %v", err)
	}
	if first["type"] != "video_url" {
		t.Errorf("first part type = %v, want video_url", first["type"])
	}
	// Check for fps field
	if first["fps"] != float64(2) {
		t.Errorf("video fps = %v, want 2", first["fps"])
	}
	if first["media_resolution"] != "default" {
		t.Errorf("video media_resolution = %v, want default", first["media_resolution"])
	}

	// No prompt — should be just the media part
	parts = buildContentParts("image", b64, mime, "")
	if len(parts) != 1 {
		t.Fatalf("buildContentParts no prompt: got %d parts, want 1", len(parts))
	}
}

func TestMediaName(t *testing.T) {
	tl := mediaTool{}
	if tl.Name() != "media" {
		t.Errorf("Name() = %q, want 'media'", tl.Name())
	}
}

func TestMediaReadOnly(t *testing.T) {
	tl := mediaTool{}
	if tl.ReadOnly() {
		t.Error("ReadOnly() = true, want false (media tool makes HTTP calls)")
	}
}

func TestMediaSchema(t *testing.T) {
	tl := mediaTool{}
	schema := tl.Schema()
	if len(schema) == 0 {
		t.Error("Schema() returned empty")
	}
	var parsed map[string]any
	if err := jsonUnmarshal(schema, &parsed); err != nil {
		t.Fatalf("Schema() is not valid JSON: %v", err)
	}
	props, ok := parsed["properties"].(map[string]any)
	if !ok {
		t.Fatal("Schema() missing properties")
	}
	for _, required := range []string{"path"} {
		if _, ok := props[required]; !ok {
			t.Errorf("Schema() missing required property %q", required)
		}
	}
}

// jsonUnmarshal is a test helper that unmarshals json.RawMessage.
func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
