//go:build windows && amd64 && reasonix_transcript_smoke

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
	"unsafe"

	"github.com/wailsapp/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"
)

var (
	user32              = windows.NewLazySystemDLL("user32.dll")
	registerClassExW    = user32.NewProc("RegisterClassExW")
	createWindowExW     = user32.NewProc("CreateWindowExW")
	destroyWindow       = user32.NewProc("DestroyWindow")
	showWindow          = user32.NewProc("ShowWindow")
	updateWindow        = user32.NewProc("UpdateWindow")
	setForegroundWindow = user32.NewProc("SetForegroundWindow")
	setFocus            = user32.NewProc("SetFocus")
	setCursorPos        = user32.NewProc("SetCursorPos")
	clientToScreen      = user32.NewProc("ClientToScreen")
	peekMessageW        = user32.NewProc("PeekMessageW")
	translateMessage    = user32.NewProc("TranslateMessage")
	dispatchMessageW    = user32.NewProc("DispatchMessageW")
	defWindowProcW      = user32.NewProc("DefWindowProcW")
	sendInput           = user32.NewProc("SendInput")
)

const (
	wsOverlappedWindow = 0x00CF0000
	swShow             = 5
	pmRemove           = 0x0001
	inputMouse         = 0
	inputKeyboard      = 1
	mouseEventFWheel   = 0x0800
	keyEventFKeyUp     = 0x0002
	keyEventFUnicode   = 0x0004
	virtualKeyBack     = 0x08
)

type smokeWindowClass struct {
	cbSize     uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   windows.Handle
	icon       windows.Handle
	cursor     windows.Handle
	background windows.Handle
	menuName   *uint16
	className  *uint16
	iconSmall  windows.Handle
}

type smokePoint struct {
	x int32
	y int32
}

type smokeMessageLoopEntry struct {
	hwnd    windows.Handle
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	point   smokePoint
	private uint32
}

type smokeMouseInput struct {
	dx        int32
	dy        int32
	mouseData uint32
	flags     uint32
	time      uint32
	extraInfo uintptr
}

type smokeInput struct {
	typeID uint32
	_      uint32
	mouse  smokeMouseInput
}

type smokeKeyboardInput struct {
	virtualKey uint16
	scanCode   uint16
	flags      uint32
	time       uint32
	extraInfo  uintptr
}

type transcriptWheelState struct {
	next       time.Time
	sustained  int
	finish     int
	finishSent bool
}

type composerKeyboardState struct {
	active     bool
	next       time.Time
	index      int
	finishSent bool
}

const (
	sustainedWheelTicks = 2500
	// Controller wheel messages can be coalesced while WebView2 commits a new
	// Virtuoso range. Keep the finishing burst bounded, but leave enough native
	// input to cross the final measured-row correction after stream growth.
	finishWheelTicks = 64
	// The injected contract has its own 80 second startup watchdog. Keep the
	// native host startup bound just above that, then grant a fresh interaction
	// budget after the contract reports the controller target. The sustained
	// wheel phase alone intentionally lasts about 40 seconds.
	transcriptSmokeStartupTimeout     = 90 * time.Second
	transcriptSmokeInteractionTimeout = 60 * time.Second
)

func (state *transcriptWheelState) advance(now time.Time) error {
	if state.sustained < sustainedWheelTicks && !now.Before(state.next) {
		if err := sendControllerWheelInput(-24); err != nil {
			return err
		}
		state.sustained++
		state.next = now.Add(16 * time.Millisecond)
		if state.sustained == sustainedWheelTicks {
			// Let reader ownership settle before proving that a bounded burst of
			// ordinary controller input can transfer to the physical tail.
			state.next = now.Add(300 * time.Millisecond)
		}
	} else if state.sustained >= sustainedWheelTicks && state.finish < finishWheelTicks && !now.Before(state.next) {
		if err := sendControllerWheelInput(-120); err != nil {
			return err
		}
		state.finish++
		state.next = now.Add(50 * time.Millisecond)
	}
	return nil
}

func (state *transcriptWheelState) shouldFinish(now time.Time) bool {
	return state.finish >= finishWheelTicks && !state.finishSent && now.After(state.next.Add(700*time.Millisecond))
}

func (state *composerKeyboardState) advance(now time.Time) error {
	if !state.active || state.finishSent || now.Before(state.next) {
		return nil
	}
	if state.index < 6 {
		if err := sendUnicodeKey(rune('a' + state.index)); err != nil {
			return err
		}
		state.index++
		state.next = now.Add(80 * time.Millisecond)
		return nil
	}
	if state.index < 12 {
		if err := sendVirtualKey(virtualKeyBack); err != nil {
			return err
		}
		state.index++
		state.next = now.Add(80 * time.Millisecond)
	}
	return nil
}

func (state *composerKeyboardState) shouldFinish(now time.Time) bool {
	return state.active && state.index >= 12 && !state.finishSent && now.After(state.next.Add(350*time.Millisecond))
}

func createTranscriptSmokeWindow() (windows.Handle, error) {
	var instance windows.Handle
	if err := windows.GetModuleHandleEx(0, nil, &instance); err != nil {
		return 0, fmt.Errorf("get module handle: %w", err)
	}
	className, _ := windows.UTF16PtrFromString(fmt.Sprintf("ReasonixTranscriptSmoke-%d", os.Getpid()))
	windowName, _ := windows.UTF16PtrFromString("Reasonix transcript native smoke")
	windowClass := smokeWindowClass{
		cbSize: uint32(unsafe.Sizeof(smokeWindowClass{})),
		wndProc: windows.NewCallback(func(hwnd windows.Handle, message uint32, wParam, lParam uintptr) uintptr {
			result, _, _ := defWindowProcW.Call(uintptr(hwnd), uintptr(message), wParam, lParam)
			return result
		}),
		instance: instance, className: className,
	}
	if registered, _, err := registerClassExW.Call(uintptr(unsafe.Pointer(&windowClass))); registered == 0 {
		return 0, fmt.Errorf("register window class: %w", err)
	}
	hwnd, _, err := createWindowExW.Call(
		0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(windowName)),
		wsOverlappedWindow, 100, 100, 1200, 800, 0, 0, uintptr(instance), 0,
	)
	if hwnd == 0 {
		return 0, fmt.Errorf("create WebView2 host window: %w", err)
	}
	showWindow.Call(hwnd, swShow)
	updateWindow.Call(hwnd)
	setForegroundWindow.Call(hwnd)
	setFocus.Call(hwnd)
	return windows.Handle(hwnd), nil
}

func transcriptSmokeTimeoutError(navigationCompleted bool, ready *smokeMessage, composerState composerKeyboardState, wheelState transcriptWheelState) error {
	phase := "startup"
	if ready != nil {
		phase = "native-input"
	}
	if wheelState.finishSent {
		phase = "result"
	}
	return fmt.Errorf(
		"WebView2 smoke timed out: phase=%s navigationCompleted=%t ready=%t composer=%t composerKeys=%d/12 composerFinishSent=%t sustained=%d/%d finish=%d/%d finishSent=%t",
		phase, navigationCompleted, ready != nil,
		composerState.active, composerState.index, composerState.finishSent,
		wheelState.sustained, sustainedWheelTicks,
		wheelState.finish, finishWheelTicks, wheelState.finishSent,
	)
}

func activateComposerKeyboard(state *composerKeyboardState, deadline *time.Time, hwnd windows.Handle, chromium *edge.Chromium) {
	*deadline = time.Now().Add(transcriptSmokeStartupTimeout)
	state.active = true
	state.next = time.Now()
	setForegroundWindow.Call(uintptr(hwnd))
	setFocus.Call(uintptr(hwnd))
	chromium.Focus()
}

func activateTranscriptWheel(message smokeMessage, ready *smokeMessage, deadline *time.Time, hwnd windows.Handle, state *transcriptWheelState) *smokeMessage {
	if ready == nil {
		*deadline = time.Now().Add(transcriptSmokeInteractionTimeout)
	}
	copy := message
	var screenPoint = smokePoint{x: int32(message.Point.X), y: int32(message.Point.Y)}
	clientToScreen.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&screenPoint)))
	setCursorPos.Call(uintptr(screenPoint.x), uintptr(screenPoint.y))
	state.next = time.Now()
	return &copy
}

func runTranscriptNativeSmoke(url, script string) (string, error) {
	if runtime.GOARCH != "amd64" {
		return "", fmt.Errorf("WebView2 smoke input layout is unsupported on %s", runtime.GOARCH)
	}
	// edge already initializes the STA on this pinned thread. A second call
	// returns successful S_FALSE (HRESULT 1), which x/sys/windows exposes as a
	// non-nil error, so the fixture must not create another COM reference.
	hwnd, err := createTranscriptSmokeWindow()
	if err != nil {
		return "", err
	}
	defer destroyWindow.Call(uintptr(hwnd))

	dataPath, err := os.MkdirTemp("", "reasonix-transcript-webview2-")
	if err != nil {
		return "", fmt.Errorf("create WebView2 data directory: %w", err)
	}
	defer os.RemoveAll(dataPath)

	messageChannel := make(chan string, 8)
	navigationChannel := make(chan struct{}, 1)
	webViewError := make(chan error, 1)
	chromium := edge.NewChromium()
	chromium.DataPath = filepath.Clean(dataPath)
	chromium.SetErrorCallback(func(err error) {
		select {
		case webViewError <- err:
		default:
		}
	})
	chromium.MessageCallback = func(message string, _ *edge.ICoreWebView2, _ *edge.ICoreWebView2WebMessageReceivedEventArgs) {
		select {
		case messageChannel <- message:
		default:
		}
	}
	chromium.NavigationCompletedCallback = func(_ *edge.ICoreWebView2, _ *edge.ICoreWebView2NavigationCompletedEventArgs) {
		select {
		case navigationChannel <- struct{}{}:
		default:
		}
	}
	if !chromium.Embed(uintptr(hwnd)) {
		return "", fmt.Errorf("embed WebView2 controller")
	}
	defer chromium.ShuttingDown()
	chromium.ResizeWithBounds(&edge.Rect{Left: 0, Top: 0, Right: 1200, Bottom: 800})
	_ = chromium.Show()
	chromium.Focus()
	chromium.Navigate(url)

	deadline := time.Now().Add(transcriptSmokeStartupTimeout)
	navigationCompleted := false
	var ready *smokeMessage
	var result string
	wheelState := transcriptWheelState{}
	composerState := composerKeyboardState{}
	for result == "" && time.Now().Before(deadline) {
		pumpWindowsMessages()
		select {
		case err := <-webViewError:
			return "", fmt.Errorf("WebView2: %w", err)
		default:
		}
		select {
		case <-navigationChannel:
			navigationCompleted = true
			chromium.Eval(script)
		default:
		}
		for {
			select {
			case raw := <-messageChannel:
				var message smokeMessage
				if jsonErr := jsonUnmarshalSmoke(raw, &message); jsonErr != nil {
					continue
				}
				switch message.Type {
				case "composer-ready":
					if !composerState.active {
						activateComposerKeyboard(&composerState, &deadline, hwnd, chromium)
					}
				case "ready":
					ready = activateTranscriptWheel(message, ready, &deadline, hwnd, &wheelState)
				case "result", "error":
					result = raw
				}
			default:
				goto messagesDrained
			}
		}
	messagesDrained:
		if ready != nil {
			now := time.Now()
			if err := wheelState.advance(now); err != nil {
				return "", err
			}
			if wheelState.shouldFinish(now) {
				wheelState.finishSent = true
				chromium.Eval("window.__reasonixNativeTranscriptSmoke.finish()")
			}
		}
		if ready == nil && composerState.active {
			now := time.Now()
			if err := composerState.advance(now); err != nil {
				return "", err
			}
			if composerState.shouldFinish(now) {
				composerState.finishSent = true
				chromium.Eval("window.__reasonixNativeTranscriptSmoke.finishComposer()")
			}
		}
		time.Sleep(time.Millisecond)
	}
	if result == "" {
		return "", transcriptSmokeTimeoutError(navigationCompleted, ready, composerState, wheelState)
	}
	return result, nil
}

func jsonUnmarshalSmoke(raw string, target *smokeMessage) error {
	return json.Unmarshal([]byte(raw), target)
}

func pumpWindowsMessages() {
	var message smokeMessageLoopEntry
	for {
		hasMessage, _, _ := peekMessageW.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0, pmRemove)
		if hasMessage == 0 {
			return
		}
		translateMessage.Call(uintptr(unsafe.Pointer(&message)))
		dispatchMessageW.Call(uintptr(unsafe.Pointer(&message)))
	}
}

// sendControllerWheelInput delivers high-resolution native wheel deltas to
// the child HWND owned by the WebView2 controller. It does not dispatch a DOM
// WheelEvent or expose any test bridge in the production frontend.
func sendControllerWheelInput(delta int32) error {
	input := smokeInput{typeID: inputMouse}
	input.mouse.mouseData = uint32(delta)
	input.mouse.flags = mouseEventFWheel
	inserted, _, err := sendInput.Call(1, uintptr(unsafe.Pointer(&input)), unsafe.Sizeof(input))
	if inserted != 1 {
		return fmt.Errorf("send WebView2 controller wheel input: %w", err)
	}
	return nil
}

func keyboardInput(input *smokeInput) *smokeKeyboardInput {
	return (*smokeKeyboardInput)(unsafe.Pointer(&input.mouse))
}

func sendUnicodeKey(key rune) error {
	var inputs [2]smokeInput
	inputs[0].typeID = inputKeyboard
	inputs[1].typeID = inputKeyboard
	keyboardInput(&inputs[0]).scanCode = uint16(key)
	keyboardInput(&inputs[0]).flags = keyEventFUnicode
	keyboardInput(&inputs[1]).scanCode = uint16(key)
	keyboardInput(&inputs[1]).flags = keyEventFUnicode | keyEventFKeyUp
	return sendKeyboardInputs(inputs[:])
}

func sendVirtualKey(key uint16) error {
	var inputs [2]smokeInput
	inputs[0].typeID = inputKeyboard
	inputs[1].typeID = inputKeyboard
	keyboardInput(&inputs[0]).virtualKey = key
	keyboardInput(&inputs[1]).virtualKey = key
	keyboardInput(&inputs[1]).flags = keyEventFKeyUp
	return sendKeyboardInputs(inputs[:])
}

func sendKeyboardInputs(inputs []smokeInput) error {
	inserted, _, err := sendInput.Call(
		uintptr(len(inputs)),
		uintptr(unsafe.Pointer(&inputs[0])),
		unsafe.Sizeof(inputs[0]),
	)
	if inserted != uintptr(len(inputs)) {
		return fmt.Errorf("send WebView2 controller keyboard input: inserted %d/%d: %w", inserted, len(inputs), err)
	}
	return nil
}
