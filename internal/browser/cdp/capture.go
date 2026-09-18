package cdp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"reasonix/internal/attachment"
	"reasonix/internal/browser"
)

// downloadPollInterval is how often a waiting browser_download re-reads the
// records the browser's progress events keep up to date.
const downloadPollInterval = 200 * time.Millisecond

// downloadRecord is one download this executor observed, keyed by the guid
// Chrome assigns it.
type downloadRecord struct {
	guid      string
	tab       string
	url       string
	suggested string
	path      string
	state     string
	received  int64
	total     int64
}

func (d *downloadRecord) download() browser.Download {
	bytesSeen := d.received
	if d.state == "completed" && d.total > 0 {
		bytesSeen = d.total
	}
	return browser.Download{ID: d.guid, URL: d.url, Path: d.path, State: d.state, Bytes: bytesSeen}
}

// Screenshot writes a PNG this task owns and names it; image bytes reach the
// model through the tool's image channel, never through a control frame.
func (e *Executor) Screenshot(ctx context.Context, req browser.ScreenshotRequest) (browser.Screenshot, error) {
	p, err := e.lookup(ctx, req.TabID)
	if err != nil {
		return browser.Screenshot{}, err
	}
	params, err := e.captureParams(ctx, p, req)
	if err != nil {
		return browser.Screenshot{}, err
	}
	var out struct {
		Data string `json:"data"`
	}
	if err := e.conn.call(ctx, p.session, "Page.captureScreenshot", params, &out); err != nil {
		return browser.Screenshot{}, fmt.Errorf("capture screenshot: %w", err)
	}
	if strings.TrimSpace(out.Data) != out.Data || out.Data == "" {
		return browser.Screenshot{}, fmt.Errorf("decode screenshot: non-canonical base64")
	}
	data, err := base64.StdEncoding.Strict().DecodeString(out.Data)
	if err != nil || base64.StdEncoding.EncodeToString(data) != out.Data {
		return browser.Screenshot{}, fmt.Errorf("decode screenshot: invalid base64")
	}
	mime, width, height, err := attachment.ValidateImage(data, "image/png", attachment.DefaultPolicy())
	if err != nil {
		return browser.Screenshot{}, fmt.Errorf("validate screenshot: %w", err)
	}
	// The tab's registered ID, not the caller's argument: a screenshot names a
	// file, and only IDs this executor minted may reach a path.
	name := fmt.Sprintf("%s-%d.png", p.id, time.Now().UnixNano())
	path, err := e.artifactPath("screenshots", name)
	if err != nil {
		return browser.Screenshot{}, err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return browser.Screenshot{}, fmt.Errorf("write screenshot: %w", err)
	}
	return browser.Screenshot{Path: path, MIME: mime, Width: width, Height: height}, nil
}

// captureParams turns the request into a capture region. Element and full-page
// clips are page coordinates, so the visual viewport's offset is added back.
func (e *Executor) captureParams(ctx context.Context, p *page, req browser.ScreenshotRequest) (map[string]any, error) {
	params := map[string]any{"format": "png"}
	if req.Ref == "" && !req.FullPage {
		return params, nil
	}
	metrics, err := e.layout(ctx, p)
	if err != nil {
		return nil, err
	}
	params["captureBeyondViewport"] = true
	if req.Ref != "" {
		rect, err := e.locate(ctx, p, req.Ref)
		if err != nil {
			return nil, err
		}
		if rect.Hidden {
			return nil, fmt.Errorf("element %s has no visible box to capture", req.Ref)
		}
		params["clip"] = map[string]any{
			"x":     rect.X - rect.Width/2 + metrics.pageX,
			"y":     rect.Y - rect.Height/2 + metrics.pageY,
			"width": rect.Width, "height": rect.Height, "scale": 1,
		}
		return params, nil
	}
	params["clip"] = map[string]any{"x": 0, "y": 0, "width": metrics.contentWidth, "height": metrics.contentHeight, "scale": 1}
	return params, nil
}

// layoutMetrics is the subset of Page.getLayoutMetrics this package uses.
type layoutMetrics struct {
	pageX, pageY                float64
	viewWidth, viewHeight       float64
	contentWidth, contentHeight float64
}

func (e *Executor) layout(ctx context.Context, p *page) (layoutMetrics, error) {
	var out struct {
		CSSContentSize struct {
			Width  float64 `json:"width"`
			Height float64 `json:"height"`
		} `json:"cssContentSize"`
		CSSVisualViewport struct {
			PageX        float64 `json:"pageX"`
			PageY        float64 `json:"pageY"`
			ClientWidth  float64 `json:"clientWidth"`
			ClientHeight float64 `json:"clientHeight"`
		} `json:"cssVisualViewport"`
	}
	if err := e.conn.call(ctx, p.session, "Page.getLayoutMetrics", nil, &out); err != nil {
		return layoutMetrics{}, fmt.Errorf("read layout metrics: %w", err)
	}
	return layoutMetrics{
		pageX: out.CSSVisualViewport.PageX, pageY: out.CSSVisualViewport.PageY,
		viewWidth: out.CSSVisualViewport.ClientWidth, viewHeight: out.CSSVisualViewport.ClientHeight,
		contentWidth: out.CSSContentSize.Width, contentHeight: out.CSSContentSize.Height,
	}, nil
}

// setDownloadBehavior points a browser context's downloads at the task's
// artifact directory. allowAndName writes each file under its guid, which is
// the only name that cannot collide before the download is finished.
func (e *Executor) setDownloadBehavior(ctx context.Context, contextID string) error {
	dir, err := e.artifactPath("downloads", "")
	if err != nil {
		return err
	}
	params := map[string]any{"behavior": "allowAndName", "downloadPath": dir, "eventsEnabled": true}
	if contextID != "" {
		params["browserContextId"] = contextID
	}
	if err := e.conn.call(ctx, "", "Browser.setDownloadBehavior", params, nil); err != nil {
		return fmt.Errorf("configure downloads: %w", err)
	}
	return nil
}

func (e *Executor) downloadWillBegin(params json.RawMessage) {
	var ev struct {
		FrameID           string `json:"frameId"`
		GUID              string `json:"guid"`
		URL               string `json:"url"`
		SuggestedFilename string `json:"suggestedFilename"`
	}
	if err := json.Unmarshal(params, &ev); err != nil || ev.GUID == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	tab := ""
	for _, p := range e.pages {
		p.mu.Lock()
		if p.doc.frame == ev.FrameID {
			tab = p.id
			p.downloads = append(p.downloads, ev.GUID)
		}
		p.mu.Unlock()
	}
	e.downloads[ev.GUID] = &downloadRecord{
		guid: ev.GUID, tab: tab, url: ev.URL, suggested: ev.SuggestedFilename,
		state: "inProgress", path: filepath.Join(e.artifacts, "downloads", ev.GUID),
	}
}

func (e *Executor) downloadProgress(params json.RawMessage) {
	var ev struct {
		GUID          string  `json:"guid"`
		TotalBytes    float64 `json:"totalBytes"`
		ReceivedBytes float64 `json:"receivedBytes"`
		State         string  `json:"state"`
	}
	if err := json.Unmarshal(params, &ev); err != nil || ev.GUID == "" {
		return
	}
	e.mu.Lock()
	record, ok := e.downloads[ev.GUID]
	if !ok {
		e.mu.Unlock()
		return
	}
	record.state = ev.State
	record.received = int64(ev.ReceivedBytes)
	record.total = int64(ev.TotalBytes)
	finished := ev.State == "completed"
	from, suggested := record.path, record.suggested
	e.mu.Unlock()
	if !finished {
		return
	}
	if to, err := renameDownload(from, suggested); err == nil {
		e.mu.Lock()
		record.path = to
		e.mu.Unlock()
	}
}

// renameDownload gives a finished download its suggested name without ever
// overwriting a file that is already there.
func renameDownload(from, suggested string) (string, error) {
	name := safeFilename(suggested)
	if name == "" {
		return from, nil
	}
	dir := filepath.Dir(from)
	target := filepath.Join(dir, name)
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		if _, err := os.Stat(target); os.IsNotExist(err) {
			break
		}
		if i > 500 {
			return from, nil
		}
		target = filepath.Join(dir, stem+" ("+strconv.Itoa(i)+")"+ext)
	}
	if err := os.Rename(from, target); err != nil {
		return from, err
	}
	return target, nil
}

// safeFilename keeps a server-suggested name from escaping the download
// directory or naming a device.
func safeFilename(name string) string {
	name = strings.TrimSpace(filepath.Base(strings.ReplaceAll(name, `\`, "/")))
	if name == "." || name == ".." || name == "/" {
		return ""
	}
	cleaned := strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, name)
	if len(cleaned) > 180 {
		cleaned = cleaned[:180]
	}
	return cleaned
}

// Downloads lists a tab's downloads, optionally waiting for the ones still
// running to settle.
func (e *Executor) Downloads(ctx context.Context, req browser.DownloadsRequest) ([]browser.Download, error) {
	p, err := e.lookup(ctx, req.TabID)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(req.WaitFor)
	for {
		list, pending := e.tabDownloads(p)
		if !pending || req.WaitFor <= 0 || time.Now().After(deadline) {
			return list, nil
		}
		select {
		case <-ctx.Done():
			return list, ctx.Err()
		case <-time.After(downloadPollInterval):
		}
	}
}

func (e *Executor) tabDownloads(p *page) ([]browser.Download, bool) {
	p.mu.Lock()
	guids := append([]string(nil), p.downloads...)
	p.mu.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	list := make([]browser.Download, 0, len(guids))
	pending := false
	for _, guid := range guids {
		record, ok := e.downloads[guid]
		if !ok {
			continue
		}
		if record.state == "inProgress" {
			pending = true
		}
		list = append(list, record.download())
	}
	return list, pending
}
