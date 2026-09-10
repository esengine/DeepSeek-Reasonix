package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"reasonix/internal/tool"
)

const screenshotMaxBytes = 8 << 20

func tabsTool(exec Executor) tool.Tool {
	return readTool{base: base{exec: exec, name: "browser_tabs",
		description: "List this task's browser tabs with ID, URL, title, and loading state. Start here to find a tabId, or call browser_open when the task has no tab yet.",
		schema:      objectSchema(nil),
		snip:        listSnip,
	}, run: runTabs}
}

func runTabs(ctx context.Context, exec Executor, args json.RawMessage) (string, error) {
	var p struct{}
	if err := decode(args, &p); err != nil {
		return "", err
	}
	tabs, err := exec.Tabs(ctx)
	if err != nil {
		return "", translate(err, "browser_tabs")
	}
	if len(tabs) == 0 {
		return "no tabs are open for this task; call browser_open to create one", nil
	}
	lines := make([]string, 0, len(tabs)+1)
	lines = append(lines, fmt.Sprintf("%d tab(s)", len(tabs)))
	for _, t := range tabs {
		lines = append(lines, formatTab(t))
	}
	return strings.Join(lines, "\n"), nil
}

func formatTab(t Tab) string {
	s := fmt.Sprintf("tab %s: %s", t.ID, t.URL)
	if t.Title != "" {
		s += fmt.Sprintf(" %q", t.Title)
	}
	var flags []string
	if t.Loading {
		flags = append(flags, "loading")
	}
	if t.Temporary {
		flags = append(flags, "temporary")
	}
	if len(flags) > 0 {
		s += " [" + strings.Join(flags, ", ") + "]"
	}
	return s
}

func snapshotTool(exec Executor) tool.Tool {
	return readTool{base: base{exec: exec, name: "browser_snapshot",
		description: "Capture the structural snapshot of a tab: an accessibility-style tree where each interactive element carries a ref such as ref=e12, plus the documentToken that binds those refs to the current document. Take a snapshot before browser_click, browser_type, browser_press, browser_scroll, browser_select, or browser_upload, and again after anything changes the page: refs and the token expire on navigation, page replacement, or user take-over. Pass selector to scope the tree to one subtree.",
		schema:      objectSchema([]string{"tabId"}, tabIDProp(), str("selector", "Optional CSS selector; only the matching subtree is captured.")),
		snip:        treeSnip,
	}, run: runSnapshot}
}

func runSnapshot(ctx context.Context, exec Executor, args json.RawMessage) (string, error) {
	var p struct {
		TabID    string `json:"tabId"`
		Selector string `json:"selector"`
	}
	if err := decode(args, &p); err != nil {
		return "", err
	}
	if err := requireTab(p.TabID); err != nil {
		return "", err
	}
	snap, err := exec.Snapshot(ctx, SnapshotRequest{TabID: p.TabID, Selector: p.Selector})
	if err != nil {
		return "", translate(err, "browser_snapshot")
	}
	return fmt.Sprintf("documentToken: %s\nurl: %s\ntitle: %s\nrefs: %d\n\n%s", snap.DocumentToken, snap.URL, snap.Title, snap.Refs, snap.Tree), nil
}

func downloadTool(exec Executor) tool.Tool {
	return readTool{base: base{exec: exec, name: "browser_download",
		description: "List the downloads of a tab, or wait up to waitSeconds for an in-progress one to finish. Each entry reports ID, URL, saved path, state, and size; the path is a task-owned file the file tools can read.",
		schema:      objectSchema([]string{"tabId"}, tabIDProp(), bounded(integer("waitSeconds", "Seconds to wait for an in-progress download to complete; 0 lists immediately."), 0, 300)),
		snip:        listSnip,
	}, run: runDownloads}
}

func runDownloads(ctx context.Context, exec Executor, args json.RawMessage) (string, error) {
	var p struct {
		TabID       string `json:"tabId"`
		WaitSeconds int    `json:"waitSeconds"`
	}
	if err := decode(args, &p); err != nil {
		return "", err
	}
	if err := requireTab(p.TabID); err != nil {
		return "", err
	}
	if p.WaitSeconds < 0 || p.WaitSeconds > 300 {
		return "", fmt.Errorf("waitSeconds must be between 0 and 300")
	}
	downloads, err := exec.Downloads(ctx, DownloadsRequest{TabID: p.TabID, WaitFor: time.Duration(p.WaitSeconds) * time.Second})
	if err != nil {
		return "", translate(err, "browser_download")
	}
	if len(downloads) == 0 {
		return "no downloads for tab " + p.TabID, nil
	}
	lines := make([]string, 0, len(downloads)+1)
	lines = append(lines, fmt.Sprintf("%d download(s) for tab %s", len(downloads), p.TabID))
	for _, d := range downloads {
		lines = append(lines, fmt.Sprintf("download %s: %s %s -> %s (%d bytes)", d.ID, d.State, d.URL, d.Path, d.Bytes))
	}
	return strings.Join(lines, "\n"), nil
}

type screenshot struct{ base }

func screenshotTool(exec Executor) tool.Tool {
	return screenshot{base{exec: exec, name: "browser_screenshot",
		description: "Capture a PNG of a tab's viewport, of one element by ref, or of the full page, and return it as an image. Use browser_snapshot for structure and refs; use this to see layout, images, or rendering. Images over 8 MiB are not returned; capture an element or the viewport instead.",
		schema:      objectSchema([]string{"tabId"}, tabIDProp(), refProp("Optional element ref from browser_snapshot; captures only that element."), boolean("fullPage", "Capture the whole scrollable page instead of the viewport.")),
		snip:        shortSnip,
	}}
}

func (screenshot) ReadOnly() bool     { return true }
func (screenshot) PlanModeSafe() bool { return true }

func (t screenshot) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	text, images, err := t.ExecuteWithImages(ctx, args)
	if err == nil && len(images) > 0 {
		text += "\nVisual content requires a structured image channel and an image-capable model."
	}
	return text, err
}

func (t screenshot) ExecuteWithImages(ctx context.Context, args json.RawMessage) (string, []string, error) {
	if err := t.ready(ctx); err != nil {
		return "", nil, err
	}
	var p struct {
		TabID    string `json:"tabId"`
		Ref      string `json:"ref"`
		FullPage bool   `json:"fullPage"`
	}
	if err := decode(args, &p); err != nil {
		return "", nil, err
	}
	if err := requireTab(p.TabID); err != nil {
		return "", nil, err
	}
	shot, err := t.exec.Screenshot(ctx, ScreenshotRequest{TabID: p.TabID, Ref: p.Ref, FullPage: p.FullPage})
	if err != nil {
		return "", nil, translate(err, "browser_screenshot")
	}
	return encodeScreenshot(p.TabID, shot)
}

func encodeScreenshot(tabID string, shot Screenshot) (string, []string, error) {
	info, err := os.Stat(shot.Path)
	if err != nil {
		return "", nil, fmt.Errorf("read screenshot %s: %w", shot.Path, err)
	}
	if !info.Mode().IsRegular() {
		return "", nil, fmt.Errorf("screenshot %s is not a regular file", shot.Path)
	}
	if info.Size() > screenshotMaxBytes {
		return oversizeText(shot.Path, info.Size()), nil, nil
	}
	f, err := os.Open(shot.Path)
	if err != nil {
		return "", nil, fmt.Errorf("read screenshot %s: %w", shot.Path, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, screenshotMaxBytes+1))
	if err != nil {
		return "", nil, fmt.Errorf("read screenshot %s: %w", shot.Path, err)
	}
	if len(data) > screenshotMaxBytes {
		return oversizeText(shot.Path, int64(len(data))), nil, nil
	}
	mime := shot.MIME
	if mime == "" {
		mime = "image/png"
	}
	text := fmt.Sprintf("[image: %s, %dx%d] screenshot of tab %s saved at %s", mime, shot.Width, shot.Height, tabID, shot.Path)
	return text, []string{"data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)}, nil
}

func oversizeText(path string, size int64) string {
	return fmt.Sprintf("screenshot %s is %d bytes, over the %d MiB limit for an inline image, so it was not returned. Capture one element with ref, or the viewport without fullPage, to get a smaller image.", path, size, screenshotMaxBytes>>20)
}
