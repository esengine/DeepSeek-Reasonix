package outbound

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"

	"github.com/larksuite/oapi-sdk-go/v3/channel/normalize"
	"github.com/larksuite/oapi-sdk-go/v3/channel/safety"
	"github.com/larksuite/oapi-sdk-go/v3/channel/types"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

// Uploader provides convenient methods to upload files and images to Lark.
type Uploader interface {
	UploadImage(ctx context.Context, imageType string, fileName string, image io.Reader) (string, error)
	UploadImagePath(ctx context.Context, imageType string, imagePath string) (string, error)
	UploadFile(ctx context.Context, fileType string, fileName string, file io.Reader, duration *int) (string, error)
	UploadFilePath(ctx context.Context, fileType string, filePath string) (string, error)
	UploadMedia(ctx context.Context, input *types.UploadInput, opts *safety.SsrfGuardOptions) (*types.UploadResult, error)
}

type uploaderImpl struct {
	client *lark.Client
}

// NewUploader creates a new Uploader instance.
func NewUploader(client *lark.Client) Uploader {
	return &uploaderImpl{client: client}
}

func (u *uploaderImpl) UploadImage(ctx context.Context, imageType string, fileName string, image io.Reader) (string, error) {
	req := larkim.NewCreateImageReqBuilder().
		Body(larkim.NewCreateImageReqBodyBuilder().
			ImageType(imageType).
			Image(image).
			Build()).
		Build()

	resp, err := u.client.Im.V1.Image.Create(ctx, req)
	if err != nil {
		return "", err
	}
	if !resp.Success() {
		return "", fmt.Errorf("upload image failed: code=%d, msg=%s", resp.Code, resp.Msg)
	}
	if resp.Data == nil || resp.Data.ImageKey == nil {
		return "", fmt.Errorf("upload image failed: empty response data")
	}

	return *resp.Data.ImageKey, nil
}

func (u *uploaderImpl) UploadImagePath(ctx context.Context, imageType string, imagePath string) (string, error) {
	file, err := os.Open(imagePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	return u.UploadImage(ctx, imageType, filepath.Base(imagePath), file)
}

func (u *uploaderImpl) UploadFile(ctx context.Context, fileType string, fileName string, file io.Reader, duration *int) (string, error) {
	builder := larkim.NewCreateFileReqBodyBuilder().
		FileType(fileType).
		FileName(fileName).
		File(file)

	if duration != nil {
		builder.Duration(*duration)
	}

	req := larkim.NewCreateFileReqBuilder().
		Body(builder.Build()).
		Build()

	resp, err := u.client.Im.V1.File.Create(ctx, req)
	if err != nil {
		return "", err
	}
	if !resp.Success() {
		return "", fmt.Errorf("upload file failed: code=%d, msg=%s", resp.Code, resp.Msg)
	}
	if resp.Data == nil || resp.Data.FileKey == nil {
		return "", fmt.Errorf("upload file failed: empty response data")
	}

	return *resp.Data.FileKey, nil
}

func (u *uploaderImpl) UploadFilePath(ctx context.Context, fileType string, filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	return u.UploadFile(ctx, fileType, filepath.Base(filePath), file, nil)
}

func (u *uploaderImpl) UploadMedia(ctx context.Context, input *types.UploadInput, opts *safety.SsrfGuardOptions) (*types.UploadResult, error) {
	var sourceBytes []byte
	var err error

	if input.SourceBytes != nil {
		sourceBytes = input.SourceBytes
	} else if input.SourcePath != "" {
		sourceBytes, err = os.ReadFile(input.SourcePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read file: %w", err)
		}
	} else if input.SourceURL != "" {
		if opts != nil {
			err = safety.AssertPublicURL(ctx, input.SourceURL, opts)
			if err != nil {
				return nil, fmt.Errorf("ssrf_blocked: %w", err)
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, input.SourceURL, nil)
		if err != nil {
			return nil, fmt.Errorf("invalid url: %w", err)
		}
		client := &http.Client{Timeout: 15 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch source url failed: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("fetch source url failed: status code %d", resp.StatusCode)
		}

		sourceBytes, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("read body failed: %w", err)
		}
	} else {
		return nil, fmt.Errorf("no source provided")
	}

	if input.Kind == types.MediaKindImage {
		fileName := input.FileName
		if fileName == "" {
			fileName = "image.png"
		}
		key, err := u.UploadImage(ctx, "message", fileName, bytes.NewReader(sourceBytes))
		if err != nil {
			return nil, err
		}
		return &types.UploadResult{Kind: types.MediaKindImage, FileKey: key}, nil
	}

	duration := input.Duration
	if (input.Kind == types.MediaKindAudio || input.Kind == types.MediaKindVideo) && (duration == nil || *duration <= 0) {
		if input.Kind == types.MediaKindAudio {
			d, err := normalize.ParseOpusDuration(bytes.NewReader(sourceBytes))
			if err == nil {
				duration = &d
			}
		} else if input.Kind == types.MediaKindVideo {
			d, err := normalize.ParseMP4Duration(bytes.NewReader(sourceBytes))
			if err == nil {
				duration = &d
			}
		}

		if duration == nil {
			return nil, fmt.Errorf("upload_failed: duration could not be determined for %s; pass it explicitly", input.Kind)
		}
	}

	fileType := "stream"
	fileName := input.FileName
	if input.Kind == types.MediaKindAudio {
		fileType = "opus"
		if fileName == "" {
			fileName = "voice.opus"
		}
	} else if input.Kind == types.MediaKindVideo {
		fileType = "mp4"
		if fileName == "" {
			fileName = "video.mp4"
		}
	} else {
		if fileName == "" {
			fileName = "upload.bin"
		}
	}

	key, err := u.UploadFile(ctx, fileType, fileName, bytes.NewReader(sourceBytes), duration)
	if err != nil {
		return nil, err
	}

	return &types.UploadResult{
		Kind:       input.Kind,
		FileKey:    key,
		DurationMs: duration,
	}, nil
}
