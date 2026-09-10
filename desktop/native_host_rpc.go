package main

import (
	"context"
	"log/slog"
	"time"

	"reasonix/desktop/internal/hostrpc"
)

// rpcNativeHost implements nativeHost over host/* requests to the Electron
// shell. Fire-and-forget window operations use a short timeout so a wedged
// shell can never block a business goroutine on the Go side.
type rpcNativeHost struct {
	server *hostrpc.Server
}

var _ nativeHost = rpcNativeHost{}

const (
	rpcHostWindowTimeout = 5 * time.Second
	rpcHostDialogTimeout = 24 * time.Hour
)

type hostValueResult struct {
	Value bool `json:"value"`
}

type hostPathResult struct {
	Path string `json:"path"`
}

type hostPathsResult struct {
	Paths []string `json:"paths"`
}

type hostButtonResult struct {
	Button string `json:"button"`
}

type hostScreenList struct {
	Screens []struct {
		X       int     `json:"x"`
		Y       int     `json:"y"`
		Width   int     `json:"width"`
		Height  int     `json:"height"`
		Scale   float64 `json:"scale"`
		Primary bool    `json:"primary"`
	} `json:"screens"`
}

type hostFileFilter struct {
	DisplayName string `json:"displayName"`
	Pattern     string `json:"pattern"`
}

type hostDialogParams struct {
	Title            string           `json:"title,omitempty"`
	DefaultDirectory string           `json:"defaultDirectory,omitempty"`
	DefaultFilename  string           `json:"defaultFilename,omitempty"`
	Filters          []hostFileFilter `json:"filters"`
	Multiple         bool             `json:"multiple,omitempty"`
}

func hostDialogParamsFrom(opts nativeDialogOptions) hostDialogParams {
	p := hostDialogParams{
		Title:            opts.Title,
		DefaultDirectory: opts.DefaultDirectory,
		DefaultFilename:  opts.DefaultFilename,
		Filters:          []hostFileFilter{},
	}
	for _, f := range opts.Filters {
		p.Filters = append(p.Filters, hostFileFilter(f))
	}
	return p
}

func (h rpcNativeHost) call(ctx context.Context, method string, params any, result any, timeout time.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	err := h.server.Request(ctx, method, params, result)
	if err != nil {
		slog.Warn("desktop host: native call failed", "method", method, "err", err)
	}
	return err
}

func (h rpcNativeHost) fire(ctx context.Context, method string, params any) {
	_ = h.call(ctx, method, params, nil, rpcHostWindowTimeout)
}

func (h rpcNativeHost) ShowWindow(ctx context.Context) {
	h.fire(ctx, "host/window.show", map[string]string{"reason": "service"})
}
func (h rpcNativeHost) ShowApplication(ctx context.Context) {
	h.fire(ctx, "host/window.show", map[string]string{"reason": "application"})
}
func (h rpcNativeHost) HideWindow(ctx context.Context)      { h.fire(ctx, "host/window.hide", struct{}{}) }
func (h rpcNativeHost) HideApplication(ctx context.Context) { h.fire(ctx, "host/app.hide", struct{}{}) }
func (h rpcNativeHost) MaximiseWindow(ctx context.Context) {
	h.fire(ctx, "host/window.maximise", struct{}{})
}
func (h rpcNativeHost) UnmaximiseWindow(ctx context.Context) {
	h.fire(ctx, "host/window.unmaximise", struct{}{})
}
func (h rpcNativeHost) MinimiseWindow(ctx context.Context) {
	h.fire(ctx, "host/window.minimise", struct{}{})
}
func (h rpcNativeHost) UnminimiseWindow(ctx context.Context) {
	h.fire(ctx, "host/window.unminimise", struct{}{})
}
func (h rpcNativeHost) ToggleMaximiseWindow(ctx context.Context) {
	h.fire(ctx, "host/window.toggleMaximise", struct{}{})
}
func (h rpcNativeHost) CenterWindow(ctx context.Context) {
	h.fire(ctx, "host/window.center", struct{}{})
}

func (h rpcNativeHost) WindowIsMaximised(ctx context.Context) bool {
	var out hostValueResult
	_ = h.call(ctx, "host/window.isMaximised", struct{}{}, &out, rpcHostWindowTimeout)
	return out.Value
}

func (h rpcNativeHost) WindowIsMinimised(ctx context.Context) bool {
	var out hostValueResult
	_ = h.call(ctx, "host/window.isMinimised", struct{}{}, &out, rpcHostWindowTimeout)
	return out.Value
}

func (h rpcNativeHost) SetWindowPosition(ctx context.Context, x, y int) {
	h.fire(ctx, "host/window.setPosition", map[string]int{"x": x, "y": y})
}

func (h rpcNativeHost) SetWindowTitle(ctx context.Context, title string) {
	h.fire(ctx, "host/window.setTitle", map[string]string{"title": title})
}

func (h rpcNativeHost) Screens(ctx context.Context) ([]nativeScreen, error) {
	var out hostScreenList
	if err := h.call(ctx, "host/screen.list", struct{}{}, &out, rpcHostWindowTimeout); err != nil {
		return nil, err
	}
	screens := make([]nativeScreen, 0, len(out.Screens))
	for _, s := range out.Screens {
		screens = append(screens, nativeScreen{Width: s.Width, Height: s.Height, Primary: s.Primary, Scale: s.Scale})
	}
	return screens, nil
}

func (h rpcNativeHost) OpenDirectoryDialog(ctx context.Context, opts nativeDialogOptions) (string, error) {
	var out hostPathResult
	err := h.call(ctx, "host/dialog.openDirectory", hostDialogParamsFrom(opts), &out, rpcHostDialogTimeout)
	return out.Path, err
}

func (h rpcNativeHost) OpenFileDialog(ctx context.Context, opts nativeDialogOptions) (string, error) {
	var out hostPathsResult
	err := h.call(ctx, "host/dialog.openFile", hostDialogParamsFrom(opts), &out, rpcHostDialogTimeout)
	if err != nil || len(out.Paths) == 0 {
		return "", err
	}
	return out.Paths[0], nil
}

func (h rpcNativeHost) SaveFileDialog(ctx context.Context, opts nativeDialogOptions) (string, error) {
	var out hostPathResult
	err := h.call(ctx, "host/dialog.saveFile", hostDialogParamsFrom(opts), &out, rpcHostDialogTimeout)
	return out.Path, err
}

func (h rpcNativeHost) MessageDialog(ctx context.Context, opts nativeMessageOptions) (string, error) {
	buttons := opts.Buttons
	if buttons == nil {
		buttons = []string{}
	}
	var out hostButtonResult
	err := h.call(ctx, "host/dialog.message", map[string]any{
		"type":          string(opts.Type),
		"title":         opts.Title,
		"message":       opts.Message,
		"buttons":       buttons,
		"defaultButton": opts.DefaultButton,
		"cancelButton":  opts.CancelButton,
	}, &out, rpcHostDialogTimeout)
	return out.Button, err
}

func (h rpcNativeHost) OpenExternal(ctx context.Context, url string) {
	h.fire(ctx, "host/shell.openExternal", map[string]string{"url": url})
}

func (h rpcNativeHost) Quit(ctx context.Context) { h.fire(ctx, "host/app.quit", struct{}{}) }

func (h rpcNativeHost) OpenDevTools(ctx context.Context) {
	h.fire(ctx, "host/devtools.toggle", struct{}{})
}
