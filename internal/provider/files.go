package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	anthropicFilesBeta = "files-api-2025-04-14"
	maxUploadFilename  = 512
)

// FileUpload is a Files API image upload (purpose=user_data).
type FileUpload struct {
	BaseURL    string
	APIKey     string
	AuthHeader bool
	Protocol   string // "anthropic" uses the Anthropic-compatible Files API
	Filename   string
	Data       []byte
	Client     *http.Client
}

type openaiFileObject struct {
	ID string `json:"id"`
}

type anthropicFileObject struct {
	ID string `json:"id"`
}

// UploadUserDataFile uploads an image and returns its file_id (file-api-…).
func UploadUserDataFile(ctx context.Context, u FileUpload) (string, error) {
	if len(u.Data) == 0 || len(u.Data) > MaxFileAPIImageBytes {
		return "", fmt.Errorf("files api image must be between 1 byte and 64 MiB")
	}
	if strings.TrimSpace(u.APIKey) == "" {
		return "", fmt.Errorf("files api: missing api key")
	}
	filename := sanitizeUploadFilename(u.Filename)
	endpoint, err := filesEndpoint(u.BaseURL, u.Protocol)
	if err != nil {
		return "", err
	}
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("purpose", "user_data"); err != nil {
		return "", err
	}
	hdr := make(textproto.MIMEHeader)
	hdr.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, escapeQuotes(filename)))
	hdr.Set("Content-Type", "application/octet-stream")
	part, err := writer.CreatePart(hdr)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(u.Data); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	anthropic := strings.EqualFold(strings.TrimSpace(u.Protocol), "anthropic")
	if anthropic {
		if u.AuthHeader {
			req.Header.Set("Authorization", "Bearer "+u.APIKey)
		} else {
			req.Header.Set("x-api-key", u.APIKey)
		}
		req.Header.Set("anthropic-beta", anthropicFilesBeta)
	} else {
		req.Header.Set("Authorization", "Bearer "+u.APIKey)
	}
	client := u.Client
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("files api: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("files api: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	id := parseUploadedFileID(raw, anthropic)
	if !IsImageFileID(id) {
		return "", fmt.Errorf("files api: missing file_id")
	}
	return id, nil
}

func parseUploadedFileID(raw []byte, anthropic bool) string {
	if anthropic {
		var obj anthropicFileObject
		if json.Unmarshal(raw, &obj) == nil {
			return strings.TrimSpace(obj.ID)
		}
	}
	var obj openaiFileObject
	if json.Unmarshal(raw, &obj) == nil {
		return strings.TrimSpace(obj.ID)
	}
	return ""
}

func filesEndpoint(baseURL, protocol string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return "", fmt.Errorf("files api: empty base url")
	}
	base = strings.TrimSuffix(base, "/v1")
	if strings.EqualFold(strings.TrimSpace(protocol), "anthropic") {
		return base + "/v1/files", nil
	}
	return base + "/files", nil
}

func sanitizeUploadFilename(name string) string {
	name = path.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
	if name == "" || name == "." || name == "/" {
		name = "image.png"
	}
	if utf8.RuneCountInString(name) > maxUploadFilename {
		runes := []rune(name)
		name = string(runes[:maxUploadFilename])
	}
	return name
}

func escapeQuotes(s string) string {
	return strings.ReplaceAll(s, `"`, `\"`)
}
