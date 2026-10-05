package browser

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"strings"

	"reasonix/internal/model/visionimage"
)

// Screenshot captures what a tab's viewport shows, sized for a vision model.
// Steps that give x and y afterwards are read in this image's pixels.
func (s *Session) Screenshot(ctx context.Context, tabID string) (string, TabInfo, error) {
	t, err := s.tab(tabID)
	if err != nil {
		return "", TabInfo{}, err
	}
	if err := t.checkCurrentURL(); err != nil {
		return "", TabInfo{}, err
	}
	var shot struct {
		Data string `json:"data"`
	}
	if err := t.call(ctx, "Page.captureScreenshot", map[string]any{"format": "jpeg", "quality": 80}, &shot); err != nil {
		return "", TabInfo{}, engineFailure(err)
	}
	raw, err := base64.StdEncoding.DecodeString(shot.Data)
	if err != nil {
		return "", TabInfo{}, fail(CodeEngineFailed, "the screenshot was not valid base64")
	}
	fitted, mime, err := visionimage.Fit(raw, "image/jpeg")
	if err != nil {
		return "", TabInfo{}, fail(CodeEngineFailed, "%v", err)
	}
	rawCfg, _, rawErr := image.DecodeConfig(bytes.NewReader(raw))
	cfg, _, err := image.DecodeConfig(bytes.NewReader(fitted))
	if rawErr != nil || err != nil || cfg.Width == 0 {
		return "", TabInfo{}, fail(CodeEngineFailed, "the screenshot could not be measured")
	}
	var dpr struct {
		Result struct {
			Value float64 `json:"value"`
		} `json:"result"`
	}
	if err := t.call(ctx, "Runtime.evaluate", map[string]any{"expression": "devicePixelRatio", "returnByValue": true}, &dpr); err != nil {
		return "", TabInfo{}, engineFailure(err)
	}
	if dpr.Result.Value > 0 {
		t.mu.Lock()
		t.shotScale = float64(rawCfg.Width) / float64(cfg.Width) / dpr.Result.Value
		t.mu.Unlock()
	}
	t.mu.Lock()
	pointer, scale := t.pointer, t.shotScale
	t.mu.Unlock()
	if pointer.visible && scale > 0 {
		marked, markErr := markPointer(fitted, pointer.x/scale, pointer.y/scale)
		if markErr == nil {
			fitted, mime = marked, "image/jpeg"
		}
	}
	var b strings.Builder
	b.WriteString("data:" + mime + ";base64,")
	b.WriteString(base64.StdEncoding.EncodeToString(fitted))
	return b.String(), t.info(true), nil
}

func markPointer(raw []byte, x, y float64) ([]byte, error) {
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	dst := image.NewRGBA(src.Bounds())
	draw.Draw(dst, dst.Bounds(), src, src.Bounds().Min, draw.Src)
	cx, cy := int(x+.5), int(y+.5)
	for radius := 10; radius >= 0; radius-- {
		ink := color.RGBA{255, 255, 255, 235}
		if radius >= 7 {
			ink = color.RGBA{20, 22, 20, 230}
		}
		r2 := radius * radius
		for py := cy - radius; py <= cy+radius; py++ {
			for px := cx - radius; px <= cx+radius; px++ {
				if (px-cx)*(px-cx)+(py-cy)*(py-cy) <= r2 && image.Pt(px, py).In(dst.Bounds()) {
					dst.Set(px, py, ink)
				}
			}
		}
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 84}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// screenshotScale converts screenshot pixels to CSS pixels. Before any
// screenshot the two are taken to be the same.
func (t *tab) screenshotScale() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.shotScale <= 0 {
		return 1
	}
	return t.shotScale
}

// Logs returns what a tab reported since the last call that read them.
func (s *Session) Logs(tabID string) ([]LogEntry, TabInfo, error) {
	t, err := s.tab(tabID)
	if err != nil {
		return nil, TabInfo{}, err
	}
	t.mu.Lock()
	mark := t.logRead
	t.mu.Unlock()
	entries, newest := t.logsAfter(mark)
	t.mu.Lock()
	t.logRead = newest
	t.mu.Unlock()
	return entries, t.info(false), nil
}
