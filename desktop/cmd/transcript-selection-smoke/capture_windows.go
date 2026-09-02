//go:build windows && reasonix_transcript_smoke

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"time"

	"github.com/wailsapp/go-webview2/pkg/edge"

	"reasonix/desktop/internal/transcriptsmoke"
)

func (host *selectionSmokeHost) captureStage(
	iteration int,
	stage captureStage,
	baseline smokeGeometry,
	baselineImage image.Image,
) (smokeFrameResult, error) {
	label := fmt.Sprintf("iteration-%d-%s", iteration, stage.label)
	snapshot, err := requestSnapshot(host.chromium, host.messages, host.webViewErrors, label, stage.frames, stage.delay)
	if err != nil {
		return smokeFrameResult{}, err
	}
	currentImage, capturePath, err := host.captureAndSave(label)
	if err != nil {
		return smokeFrameResult{}, err
	}
	pixelResult, diff, err := transcriptsmoke.Compare(
		baselineImage, currentImage, snapshot.Geometry.Shell, selectionMasks(snapshot.Geometry),
		transcriptsmoke.CompareOptions{
			ChannelThreshold: 12, MaxChangedRatio: 0.0005, MaxConnectedComponent: 128,
			ViewportWidth: snapshot.Geometry.Viewport.Width, ViewportHeight: snapshot.Geometry.Viewport.Height,
		},
	)
	if err != nil {
		return smokeFrameResult{}, err
	}
	frame := smokeFrameResult{
		Label: label, CapturePath: capturePath, Geometry: snapshot.Geometry,
		EventSamples: snapshot.EventSamples, Pixel: pixelResult,
		GeometryOK: geometryStable(baseline, snapshot.Geometry) && eventSamplesStable(baseline, snapshot.EventSamples),
	}
	if pixelResult.Passed && frame.GeometryOK {
		return frame, nil
	}
	frame.DiffPath = filepath.Join(host.artifacts, label+"-diff.png")
	if err := writePNG(frame.DiffPath, diff); err != nil {
		return smokeFrameResult{}, err
	}
	return frame, nil
}

func (host *selectionSmokeHost) captureAndSave(label string) (image.Image, string, error) {
	data, err := capturePreview(host.chromium)
	if err != nil {
		return nil, "", fmt.Errorf("capture %s: %w", label, err)
	}
	path := filepath.Join(host.artifacts, label+".png")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return nil, "", err
	}
	value, err := decodePNG(data)
	return value, path, err
}

func (host *selectionSmokeHost) warmCompositor() error {
	for attempt := 1; attempt <= 3; attempt++ {
		if _, err := capturePreview(host.chromium); err != nil {
			return fmt.Errorf("warm WebView2 compositor (attempt %d): %w", attempt, err)
		}
		pumpFor(100 * time.Millisecond)
	}
	return nil
}

func selectionMasks(geometry smokeGeometry) []transcriptsmoke.Rect {
	masks := make([]transcriptsmoke.Rect, 0, len(geometry.SelectionRects)+1)
	for _, selectionRect := range geometry.SelectionRects {
		masks = append(masks, selectionRect.Expand(6))
	}
	if geometry.Toolbar != nil {
		masks = append(masks, geometry.Toolbar.Expand(24))
	}
	return masks
}

func requestSnapshot(
	chromium *edge.Chromium,
	messages <-chan string,
	webViewErrors <-chan error,
	label string,
	frames int,
	delay int,
) (smokeMessage, error) {
	encodedLabel, _ := json.Marshal(label)
	chromium.Eval(fmt.Sprintf("window.__reasonixSelectionSmoke.snapshot(%s,%d,%d)", encodedLabel, frames, delay))
	message, err := waitForMessage(messages, webViewErrors, "snapshot", interactionTimeout)
	if err != nil {
		return smokeMessage{}, err
	}
	if message.Geometry.Label != label {
		return smokeMessage{}, fmt.Errorf("snapshot label mismatch: got %q, want %q", message.Geometry.Label, label)
	}
	return message, nil
}

func waitForMessage(messages <-chan string, webViewErrors <-chan error, messageType string, timeout time.Duration) (smokeMessage, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pumpWindowsMessages()
		if err := pollWebViewError(webViewErrors); err != nil {
			return smokeMessage{}, err
		}
		message, found, err := pollSmokeMessage(messages)
		if err != nil {
			return smokeMessage{}, err
		}
		if found && message.Type == "error" {
			return smokeMessage{}, fmt.Errorf("fixture: %s", message.Message)
		}
		if found && message.Type == messageType {
			return message, nil
		}
		time.Sleep(time.Millisecond)
	}
	return smokeMessage{}, fmt.Errorf("timed out waiting for fixture message %q", messageType)
}

func pollSmokeMessage(messages <-chan string) (smokeMessage, bool, error) {
	select {
	case raw := <-messages:
		var message smokeMessage
		if err := json.Unmarshal([]byte(raw), &message); err != nil {
			return smokeMessage{}, false, fmt.Errorf("decode fixture message %q: %w", raw, err)
		}
		return message, true, nil
	default:
		return smokeMessage{}, false, nil
	}
}

func pollWebViewError(errors <-chan error) error {
	select {
	case err := <-errors:
		return fmt.Errorf("WebView2: %w", err)
	default:
		return nil
	}
}

func capturePreview(chromium *edge.Chromium) ([]byte, error) {
	capture, err := chromium.StartSmokePreviewCapture()
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(previewCaptureTimeout)
	for time.Now().Before(deadline) {
		pumpWindowsMessages()
		data, done, pollErr := capture.Poll()
		if done {
			return data, pollErr
		}
		time.Sleep(time.Millisecond)
	}
	return nil, fmt.Errorf("CapturePreview timed out after %s", previewCaptureTimeout)
}

func decodePNG(data []byte) (image.Image, error) {
	value, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode WebView2 preview PNG: %w", err)
	}
	return value, nil
}

func writePNG(path string, value image.Image) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	encodeErr := png.Encode(file, value)
	closeErr := file.Close()
	if encodeErr != nil {
		return encodeErr
	}
	return closeErr
}

func geometryStable(baseline, current smokeGeometry) bool {
	return baseline.ScrollTop == current.ScrollTop &&
		baseline.ScrollHeight == current.ScrollHeight &&
		baseline.ClientHeight == current.ClientHeight &&
		current.HostCount == 1 && current.HostStable &&
		rectDelta(baseline.Table, current.Table) <= 0.5 &&
		rectDelta(baseline.Row, current.Row) <= 0.5 &&
		rectDelta(baseline.Target, current.Target) <= 0.5
}

func eventSamplesStable(baseline smokeGeometry, samples []smokeGeometry) bool {
	if len(samples) == 0 {
		return false
	}
	for _, sample := range samples {
		if !geometryStable(baseline, sample) {
			return false
		}
	}
	return true
}

func rectDelta(left, right transcriptsmoke.Rect) float64 {
	maximum := 0.0
	for _, value := range []float64{
		abs(left.Left - right.Left), abs(left.Top - right.Top),
		abs(left.Right - right.Right), abs(left.Bottom - right.Bottom),
	} {
		if value > maximum {
			maximum = value
		}
	}
	return maximum
}

func abs(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}
