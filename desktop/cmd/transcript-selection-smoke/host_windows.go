//go:build windows && reasonix_transcript_smoke

package main

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"time"

	"github.com/wailsapp/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"
)

const (
	startupTimeout        = 60 * time.Second
	interactionTimeout    = 15 * time.Second
	previewCaptureTimeout = 10 * time.Second
)

type selectionSmokeHost struct {
	chromium      *edge.Chromium
	messages      chan string
	webViewErrors chan error
	artifacts     string
}

type captureStage struct {
	label  string
	frames int
	delay  int
}

var selectionCaptureStages = []captureStage{
	{"pointer-up", 0, 0},
	{"raf-1", 1, 0},
	{"raf-2", 1, 0},
	{"after-100ms", 0, 100},
	{"after-300ms", 0, 300},
}

func runSelectionSmoke(url, script, artifacts string, iterations, width, height int, commit string) (smokeResult, error) {
	result := smokeResult{Commit: commit, ClickIntervals: []int{400, 320, 180}}
	if iterations <= 0 || width <= 0 || height <= 0 {
		return result, fmt.Errorf("iterations and viewport dimensions must be positive")
	}
	if err := os.MkdirAll(artifacts, 0o755); err != nil {
		return result, fmt.Errorf("create artifact directory: %w", err)
	}
	hwnd, err := createSmokeWindow(width, height)
	if err != nil {
		return result, err
	}
	defer destroyWindow.Call(uintptr(hwnd))
	dataPath, err := os.MkdirTemp("", "reasonix-selection-webview2-")
	if err != nil {
		return result, fmt.Errorf("create WebView2 data directory: %w", err)
	}
	defer os.RemoveAll(dataPath)

	host, ready, err := startSelectionSmokeHost(hwnd, url, script, dataPath, artifacts, width, height)
	if err != nil {
		return result, err
	}
	defer host.chromium.ShuttingDown()
	if ready.Platform != "windows" {
		return result, fmt.Errorf("selection fixture reported platform %q, expected windows", ready.Platform)
	}
	result.WebView2Runtime = host.chromium.SmokeRuntimeVersion()
	version := windows.RtlGetVersion()
	result.WindowsVersion = fmt.Sprintf("%d.%d.%d", version.MajorVersion, version.MinorVersion, version.BuildNumber)
	result.Viewport = ready.Geometry.Viewport
	result.DPR = ready.Geometry.DPR
	if err := host.warmCompositor(); err != nil {
		return result, err
	}
	settled, err := host.settleTarget()
	if err != nil {
		return result, err
	}
	movePointerToClientPoint(hwnd, settled.Point)
	result.Passed = true
	for iteration := 1; iteration <= iterations; iteration++ {
		iterationResult, iterationErr := host.runIteration(iteration)
		if iterationErr != nil {
			return result, iterationErr
		}
		result.Iterations = append(result.Iterations, iterationResult)
		result.Passed = result.Passed && iterationResult.Passed
	}
	return result, nil
}

func (host *selectionSmokeHost) settleTarget() (smokeMessage, error) {
	host.chromium.Eval("window.__reasonixSelectionSmoke.settle()")
	message, err := waitForMessage(host.messages, host.webViewErrors, "settled", interactionTimeout)
	if err != nil {
		return smokeMessage{}, fmt.Errorf("settle selection target after compositor warmup: %w", err)
	}
	return message, nil
}

func startSelectionSmokeHost(
	hwnd windows.Handle,
	url, script, dataPath, artifacts string, width, height int,
) (*selectionSmokeHost, smokeMessage, error) {
	host := &selectionSmokeHost{
		chromium: edge.NewChromium(), messages: make(chan string, 256),
		webViewErrors: make(chan error, 1), artifacts: artifacts,
	}
	navigated := make(chan struct{}, 1)
	host.chromium.DataPath = filepath.Clean(dataPath)
	host.chromium.SetErrorCallback(func(err error) { offerError(host.webViewErrors, err) })
	host.chromium.MessageCallback = func(message string, _ *edge.ICoreWebView2, _ *edge.ICoreWebView2WebMessageReceivedEventArgs) {
		select {
		case host.messages <- message:
		default:
		}
	}
	host.chromium.NavigationCompletedCallback = func(_ *edge.ICoreWebView2, _ *edge.ICoreWebView2NavigationCompletedEventArgs) {
		select {
		case navigated <- struct{}{}:
		default:
		}
	}
	if !host.chromium.Embed(uintptr(hwnd)) {
		return nil, smokeMessage{}, fmt.Errorf("embed WebView2 controller")
	}
	host.chromium.ResizeWithBounds(&edge.Rect{Left: 0, Top: 0, Right: int32(width), Bottom: int32(height)})
	_ = host.chromium.Show()
	host.chromium.Focus()
	host.chromium.Navigate(url)
	ready, err := waitForSelectionFixture(host, navigated, script)
	if err != nil {
		host.chromium.ShuttingDown()
		return nil, smokeMessage{}, err
	}
	return host, ready, nil
}

func waitForSelectionFixture(host *selectionSmokeHost, navigated <-chan struct{}, script string) (smokeMessage, error) {
	deadline := time.Now().Add(startupTimeout)
	injected := false
	for time.Now().Before(deadline) {
		pumpWindowsMessages()
		if err := pollWebViewError(host.webViewErrors); err != nil {
			return smokeMessage{}, err
		}
		if !injected {
			select {
			case <-navigated:
				injected = true
				host.chromium.Eval(script)
			default:
			}
		}
		message, found, err := pollSmokeMessage(host.messages)
		if err != nil {
			return smokeMessage{}, err
		}
		if found && message.Type == "error" {
			return smokeMessage{}, fmt.Errorf("fixture: %s", message.Message)
		}
		if found && message.Type == "ready" {
			return message, nil
		}
		time.Sleep(time.Millisecond)
	}
	return smokeMessage{}, fmt.Errorf("selection fixture did not become ready within %s", startupTimeout)
}

func (host *selectionSmokeHost) runIteration(iteration int) (smokeIterationResult, error) {
	result := smokeIterationResult{Iteration: iteration, Passed: true}
	if iteration > 1 {
		host.chromium.Eval(fmt.Sprintf("window.__reasonixSelectionSmoke.reset(%d)", iteration))
		if _, err := waitForMessage(host.messages, host.webViewErrors, "reset", interactionTimeout); err != nil {
			return result, err
		}
		pumpFor(600 * time.Millisecond)
	}
	baseline, err := requestSnapshot(
		host.chromium, host.messages, host.webViewErrors, fmt.Sprintf("iteration-%d-baseline", iteration), 0, 0,
	)
	if err != nil {
		return result, err
	}
	baselineImage, baselinePath, err := host.captureAndSave(fmt.Sprintf("iteration-%d-baseline", iteration))
	if err != nil {
		return result, err
	}
	result.Baseline = baselinePath
	if err := sendNativeClicksBeforeFinal(); err != nil {
		return result, err
	}
	if err := sendLeftButton(mouseEventFLeftDown); err != nil {
		return result, err
	}
	pumpFor(30 * time.Millisecond)
	downFrame, err := host.captureStage(
		iteration, captureStage{"pointer-down", 0, 0}, baseline.Geometry, baselineImage,
	)
	if err != nil {
		return result, err
	}
	result.Frames = append(result.Frames, downFrame)
	result.Passed = result.Passed && downFrame.Pixel.Passed && downFrame.GeometryOK
	if err := sendLeftButton(mouseEventFLeftUp); err != nil {
		return result, err
	}
	return host.captureIterationStages(iteration, baseline.Geometry, baselineImage, result, time.Now())
}

func (host *selectionSmokeHost) captureIterationStages(
	iteration int,
	baseline smokeGeometry,
	baselineImage image.Image,
	result smokeIterationResult,
	pointerUpAt time.Time,
) (smokeIterationResult, error) {
	for _, stage := range selectionCaptureStages {
		if stage.delay > 0 {
			if remaining := time.Until(pointerUpAt.Add(time.Duration(stage.delay) * time.Millisecond)); remaining > 0 {
				pumpFor(remaining)
			}
		}
		capture := stage
		capture.delay = 0
		frame, err := host.captureStage(iteration, capture, baseline, baselineImage)
		if err != nil {
			return result, err
		}
		result.Frames = append(result.Frames, frame)
		result.Passed = result.Passed && frame.Pixel.Passed && frame.GeometryOK
	}
	return result, nil
}

func offerError(target chan<- error, err error) {
	select {
	case target <- err:
	default:
	}
}
