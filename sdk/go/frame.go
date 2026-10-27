package extension

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var frameIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// PublishFrame publishes or replaces a frame surface: a document the plugin
// package declares under contributes.frames, shown with these props. The host
// serves the document itself; only JSON travels here. A host whose
// HostCapabilities.UIFrames is false shows Fallback instead.
func (HostUI) PublishFrame(ctx context.Context, sessionID string, generation uint64, surfaceID string, p UIFramePayload) error {
	if !frameIDPattern.MatchString(p.Frame) {
		return fmt.Errorf("extension: frame id %q is not a declared-style slug", p.Frame)
	}
	if strings.TrimSpace(p.Title) == "" || strings.TrimSpace(p.Fallback) == "" {
		return errors.New("extension: frame payload requires a title and a fallback")
	}
	if len(p.Fallback) > 600 {
		return errors.New("extension: frame fallback is longer than 600 bytes")
	}
	if len(p.Props) > 0 {
		if len(p.Props) > 64<<10 {
			return errors.New("extension: frame props are larger than 64 KiB")
		}
		if t := bytes.TrimSpace(p.Props); len(t) == 0 || t[0] != '{' {
			return errors.New("extension: frame props must be a JSON object")
		}
	}
	return publishSurface(ctx, sessionID, generation, surfaceID, UISurfaceFrame, p)
}
