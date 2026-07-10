package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/bot"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

const (
	maxOutboundMediaBytes      = 25 * 1024 * 1024
	maxOutboundMediaTotalBytes = maxOutboundMediaBytes
)

type outboundMedia struct {
	name string
	data []byte
}

// loadOutboundMedia validates and reads every requested item before any remote
// message is sent. This keeps local policy failures from producing a successful
// text message followed by a retryable /send error.
func (a *adapter) loadOutboundMedia(refs []string) ([]outboundMedia, error) {
	media := make([]outboundMedia, 0, len(refs))
	totalBytes := 0
	for _, ref := range refs {
		data, name, err := a.readOutboundFile(ref)
		if err != nil {
			return nil, err
		}
		if len(data) > maxOutboundMediaTotalBytes-totalBytes {
			return nil, fmt.Errorf("feishu outbound media: total payload must not exceed 25 MB")
		}
		totalBytes += len(data)
		media = append(media, outboundMedia{name: name, data: data})
	}
	return media, nil
}

func (a *adapter) sendMedia(ctx context.Context, msg bot.OutboundMessage, media []outboundMedia) (bot.SendResult, error) {
	var result bot.SendResult
	for _, item := range media {
		res, err := a.sendOneMedia(ctx, msg, item)
		if err != nil {
			a.logger.Warn("feishu media send failed", "err", err)
			return result, err
		}
		result.Merge(res)
	}
	return result, nil
}

func (a *adapter) sendOneMedia(ctx context.Context, msg bot.OutboundMessage, media outboundMedia) (bot.SendResult, error) {
	mimeType := http.DetectContentType(media.data[:min(len(media.data), 512)])
	if strings.HasPrefix(mimeType, "image/") {
		imageKey, err := a.uploadImage(ctx, media.data)
		if err == nil {
			content, _ := json.Marshal(map[string]string{"image_key": imageKey})
			return a.sendSDKContent(ctx, msg, larkim.MsgTypeImage, string(content))
		}
		a.logger.Warn("feishu image upload failed; falling back to file", "err", err)
	}
	fileKey, err := a.uploadFile(ctx, media.name, media.data)
	if err != nil {
		return bot.SendResult{}, err
	}
	content, _ := json.Marshal(map[string]string{"file_key": fileKey})
	return a.sendSDKContent(ctx, msg, larkim.MsgTypeFile, string(content))
}

// readOutboundFile reads a bare filename from exactly one configured root.
// os.Root pins each root directory and prevents symlink traversal outside it;
// the selected file is then sized and read through the same open handle.
func (a *adapter) readOutboundFile(ref string) ([]byte, string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, "", fmt.Errorf("feishu outbound media: empty ref")
	}
	if len(a.cfg.OutboundMediaRoots) == 0 {
		return nil, "", fmt.Errorf("feishu outbound media: local file sending is disabled (set outbound_media_roots)")
	}
	name := filepath.Base(ref)
	if name != ref || name == "." || name == ".." || name == string(filepath.Separator) {
		return nil, "", fmt.Errorf("feishu outbound media: ref must be a bare file name")
	}

	var selected *os.File
	var selectedSize int64
	for i, root := range a.cfg.OutboundMediaRoots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		if !filepath.IsAbs(root) {
			return nil, "", fmt.Errorf("feishu outbound media: configured root %d must be absolute", i+1)
		}
		rootHandle, err := os.OpenRoot(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, "", fmt.Errorf("feishu outbound media: configured root %d is unavailable: %w", i+1, err)
		}
		file, openErr := rootHandle.Open(name)
		closeErr := rootHandle.Close()
		if openErr != nil {
			if os.IsNotExist(openErr) {
				continue
			}
			return nil, "", fmt.Errorf("feishu outbound media: cannot open %q in configured root %d: %w", name, i+1, openErr)
		}
		if closeErr != nil {
			_ = file.Close()
			return nil, "", fmt.Errorf("feishu outbound media: close configured root %d: %w", i+1, closeErr)
		}
		info, err := file.Stat()
		if err != nil {
			_ = file.Close()
			return nil, "", fmt.Errorf("feishu outbound media: stat %q: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			_ = file.Close()
			continue
		}
		if selected != nil {
			_ = file.Close()
			return nil, "", fmt.Errorf("feishu outbound media: %q exists in more than one configured root", name)
		}
		selected = file
		selectedSize = info.Size()
		defer selected.Close()
	}
	if selected == nil {
		return nil, "", fmt.Errorf("feishu outbound media: %q not found in any configured root", name)
	}
	if selectedSize == 0 || selectedSize > maxOutboundMediaBytes {
		return nil, "", fmt.Errorf("feishu outbound media: %q must be between 1 byte and 25 MB", name)
	}

	raw, err := io.ReadAll(io.LimitReader(selected, maxOutboundMediaBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("feishu outbound media: read %q: %w", name, err)
	}
	if len(raw) == 0 || len(raw) > maxOutboundMediaBytes {
		return nil, "", fmt.Errorf("feishu outbound media: %q must be between 1 byte and 25 MB", name)
	}
	return raw, name, nil
}

func (a *adapter) uploadImage(ctx context.Context, data []byte) (string, error) {
	client, err := a.sdkClient()
	if err != nil {
		return "", err
	}
	var key string
	err = withTransientRetry(ctx, a.logger, "upload image", func(ctx context.Context) error {
		req := larkim.NewCreateImageReqBuilder().
			Body(larkim.NewCreateImageReqBodyBuilder().
				ImageType(larkim.CreateImageImageTypeMessage).
				Image(bytes.NewReader(data)).
				Build()).
			Build()
		resp, err := client.Im.Image.Create(ctx, req)
		if err != nil {
			return err
		}
		if resp == nil {
			return fmt.Errorf("feishu image upload error: empty response")
		}
		if !resp.Success() {
			return fmt.Errorf("feishu image upload error: %s", feishuCodeError(resp.Code, resp.Msg))
		}
		if resp.Data == nil || resp.Data.ImageKey == nil {
			return fmt.Errorf("feishu image upload error: missing image key")
		}
		key = *resp.Data.ImageKey
		return nil
	})
	return key, err
}

func (a *adapter) uploadFile(ctx context.Context, name string, data []byte) (string, error) {
	client, err := a.sdkClient()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(name) == "" {
		name = "media.bin"
	}
	var key string
	err = withTransientRetry(ctx, a.logger, "upload file", func(ctx context.Context) error {
		req := larkim.NewCreateFileReqBuilder().
			Body(larkim.NewCreateFileReqBodyBuilder().
				FileType(feishuFileType(name)).
				FileName(name).
				File(bytes.NewReader(data)).
				Build()).
			Build()
		resp, err := client.Im.File.Create(ctx, req)
		if err != nil {
			return err
		}
		if resp == nil {
			return fmt.Errorf("feishu file upload error: empty response")
		}
		if !resp.Success() {
			return fmt.Errorf("feishu file upload error: %s", feishuCodeError(resp.Code, resp.Msg))
		}
		if resp.Data == nil || resp.Data.FileKey == nil {
			return fmt.Errorf("feishu file upload error: missing file key")
		}
		key = *resp.Data.FileKey
		return nil
	})
	return key, err
}

func feishuFileType(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".pdf":
		return "pdf"
	case ".doc", ".docx":
		return "doc"
	case ".xls", ".xlsx":
		return "xls"
	case ".ppt", ".pptx":
		return "ppt"
	case ".mp4":
		return "mp4"
	case ".opus":
		return "opus"
	default:
		return "stream"
	}
}
