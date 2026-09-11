package main

import "context"

// nativeHost is the only route from business code to the shell toolkit. Each
// method maps 1:1 onto a host/* call in docs/DESKTOP_HOST_PROTOCOL.md, served
// by the Electron shell through rpcNativeHost; tests substitute fakes.
type nativeHost interface {
	ShowWindow(ctx context.Context)
	ShowApplication(ctx context.Context)
	HideWindow(ctx context.Context)
	HideApplication(ctx context.Context)
	MaximiseWindow(ctx context.Context)
	UnmaximiseWindow(ctx context.Context)
	MinimiseWindow(ctx context.Context)
	UnminimiseWindow(ctx context.Context)
	ToggleMaximiseWindow(ctx context.Context)
	CenterWindow(ctx context.Context)
	WindowIsMaximised(ctx context.Context) bool
	WindowIsMinimised(ctx context.Context) bool
	SetWindowPosition(ctx context.Context, x, y int)
	SetWindowTitle(ctx context.Context, title string)
	Screens(ctx context.Context) ([]nativeScreen, error)
	OpenDirectoryDialog(ctx context.Context, opts nativeDialogOptions) (string, error)
	OpenFileDialog(ctx context.Context, opts nativeDialogOptions) (string, error)
	SaveFileDialog(ctx context.Context, opts nativeDialogOptions) (string, error)
	MessageDialog(ctx context.Context, opts nativeMessageOptions) (string, error)
	OpenExternal(ctx context.Context, url string)
	Quit(ctx context.Context)
	OpenDevTools(ctx context.Context)
}

type nativeScreen struct {
	Width   int
	Height  int
	Primary bool
	Scale   float64
}

type nativeFileFilter struct {
	DisplayName string
	Pattern     string // semicolon-separated globs, e.g. "*.png;*.jpg"
}

type nativeDialogOptions struct {
	Title                string
	DefaultDirectory     string
	DefaultFilename      string
	Filters              []nativeFileFilter
	CanCreateDirectories bool
}

type nativeDialogType string

const (
	nativeDialogInfo     nativeDialogType = "info"
	nativeDialogWarning  nativeDialogType = "warning"
	nativeDialogError    nativeDialogType = "error"
	nativeDialogQuestion nativeDialogType = "question"
)

type nativeMessageOptions struct {
	Type          nativeDialogType
	Title         string
	Message       string
	Buttons       []string
	DefaultButton string
	CancelButton  string
}

// noopNativeHost stands in when no shell is attached (test-constructed Apps,
// nil receivers) so App methods never reach a toolkit that is not running.
type noopNativeHost struct{}

var _ nativeHost = noopNativeHost{}

func (noopNativeHost) ShowWindow(context.Context)                  {}
func (noopNativeHost) ShowApplication(context.Context)             {}
func (noopNativeHost) HideWindow(context.Context)                  {}
func (noopNativeHost) HideApplication(context.Context)             {}
func (noopNativeHost) MaximiseWindow(context.Context)              {}
func (noopNativeHost) UnmaximiseWindow(context.Context)            {}
func (noopNativeHost) MinimiseWindow(context.Context)              {}
func (noopNativeHost) UnminimiseWindow(context.Context)            {}
func (noopNativeHost) ToggleMaximiseWindow(context.Context)        {}
func (noopNativeHost) CenterWindow(context.Context)                {}
func (noopNativeHost) WindowIsMaximised(context.Context) bool      { return false }
func (noopNativeHost) WindowIsMinimised(context.Context) bool      { return false }
func (noopNativeHost) SetWindowPosition(context.Context, int, int) {}
func (noopNativeHost) SetWindowTitle(context.Context, string)      {}
func (noopNativeHost) OpenExternal(context.Context, string)        {}
func (noopNativeHost) Quit(context.Context)                        {}
func (noopNativeHost) OpenDevTools(context.Context)                {}

func (noopNativeHost) Screens(context.Context) ([]nativeScreen, error) { return nil, nil }

func (noopNativeHost) OpenDirectoryDialog(context.Context, nativeDialogOptions) (string, error) {
	return "", nil
}

func (noopNativeHost) OpenFileDialog(context.Context, nativeDialogOptions) (string, error) {
	return "", nil
}

func (noopNativeHost) SaveFileDialog(context.Context, nativeDialogOptions) (string, error) {
	return "", nil
}

func (noopNativeHost) MessageDialog(context.Context, nativeMessageOptions) (string, error) {
	return "", nil
}

func (a *App) nativeHost() nativeHost {
	if a == nil || a.host == nil {
		return noopNativeHost{}
	}
	return a.host
}

func (a *App) setNativeHost(h nativeHost) {
	a.host = h
}
