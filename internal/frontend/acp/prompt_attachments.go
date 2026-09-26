package acp

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"

	"reasonix/internal/session/control"
)

// promptText turns a prompt's blocks into the text of one turn. Bytes a block
// carries are stored under root as attachments and referenced by "@path", the
// form every other frontend hands a turn, so the turn resolves them the same
// way. Audio is not advertised and is ignored.
func promptText(root string, blocks []ContentBlock) (string, error) {
	parts := make([]string, 0, len(blocks))
	for i, b := range blocks {
		part, err := blockText(root, b)
		if err != nil {
			return "", fmt.Errorf("prompt block %d (%s): %w", i, b.Type, err)
		}
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n")), nil
}

func blockText(root string, b ContentBlock) (string, error) {
	switch b.Type {
	case "text":
		return b.Text, nil
	case "image":
		return attach(root, b.MimeType, "", b.Data)
	case "resource":
		if b.Resource == nil {
			return "", nil
		}
		if b.Resource.Text != "" {
			return b.Resource.Text, nil
		}
		if b.Resource.Blob != "" {
			return attach(root, b.Resource.MimeType, resourceName(b.Resource.URI), b.Resource.Blob)
		}
	}
	return "", nil
}

// attach stores base64 bytes and returns the reference a turn resolves. An
// image type must sniff as an image; anything else keeps the name's extension.
func attach(root, mime, name, data string) (string, error) {
	if data == "" {
		return "", errors.New("no data")
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return "", errors.New("data must be base64")
	}
	var rel string
	if name == "" || strings.HasPrefix(strings.ToLower(mime), "image/") {
		rel, err = control.SaveImageBytesInRoot(root, mime, raw)
	} else {
		rel, err = control.SaveAttachmentBytesInRoot(root, name, raw)
	}
	if err != nil {
		return "", err
	}
	return "@" + rel, nil
}

func resourceName(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Path == "" {
		return "resource"
	}
	return path.Base(u.Path)
}
