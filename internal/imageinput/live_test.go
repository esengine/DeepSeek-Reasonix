//go:build live

package imageinput_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/imageinput"
	"reasonix/internal/provider"
	"reasonix/internal/provider/anthropic"
	"reasonix/internal/provider/openai"
	"reasonix/internal/provider/responses"
)

func fixture(t *testing.T) (string, string) {
	t.Helper()
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	code := ""
	for _, v := range b {
		code += string("KRTXYZ"[int(v)%6])
	}
	im := image.NewRGBA(image.Rect(0, 0, 400, 160))
	draw.Draw(im, im.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(im, image.Rect(10, 70, 180, 150), image.NewUniform(color.RGBA{255, 0, 0, 255}), image.Point{}, draw.Src)
	draw.Draw(im, image.Rect(220, 70, 390, 150), image.NewUniform(color.RGBA{0, 0, 255, 255}), image.Point{}, draw.Src)
	// Scale a small bitmap font so random text is legible without external assets.
	label := image.NewRGBA(image.Rect(0, 0, 80, 20))
	draw.Draw(label, label.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	d := font.Drawer{Dst: label, Src: image.NewUniform(color.Black), Face: basicfont.Face7x13, Dot: fixed.P(2, 14)}
	d.DrawString(strings.Join(strings.Split(code, ""), "  "))
	for y := range 60 {
		for x := range 240 {
			im.Set(60+x, y, label.At(x/3, y/3))
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, im); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(out.Bytes()), code
}
func collect(t *testing.T, p provider.Provider, r provider.Request) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	ch, err := p.Stream(ctx, r)
	if err != nil {
		t.Fatalf("request failed (%T); no request or credentials logged", err)
	}
	var out strings.Builder
	for {
		select {
		case <-ctx.Done():
			t.Fatal("request timed out")
		case c, ok := <-ch:
			if !ok {
				return out.String()
			}
			if c.Err != nil {
				t.Fatalf("stream failed (%T); no payload logged", c.Err)
			}
			if c.Type == provider.ChunkText {
				out.WriteString(c.Text)
			}
		}
	}
}
func TestLiveToolImages(t *testing.T) {
	if os.Getenv("REASONIX_LIVE_TOOL_IMAGES") != "1" {
		t.Skip("live probe disabled")
	}
	cfg, err := config.LoadForRootReadOnly("../..")
	if err != nil {
		t.Fatal("configuration unavailable")
	}
	key := ""
	for _, p := range cfg.Providers {
		if openai.IsDeepSeek(p.BaseURL) && p.APIKey() != "" {
			key = p.APIKey()
			break
		}
	}
	if key == "" {
		t.Skip("official DeepSeek credentials unavailable")
	}
	makeProvider := func(protocol, model string) provider.Provider {
		cfg := provider.Config{Name: "deepseek", BaseURL: "https://api.deepseek.com", Model: model, APIKey: key, Extra: map[string]any{"thinking": "disabled"}}
		if protocol == "responses" {
			return responses.New(responses.Config{Name: "deepseek", BaseURL: cfg.BaseURL, Model: model, APIKey: key, Mode: "stateless", MaxOutputTokens: 2048, Extra: cfg.Extra})
		}
		var p provider.Provider
		var e error
		if protocol == "anthropic" {
			cfg.BaseURL += "/anthropic"
			p, e = anthropic.New(cfg)
		} else {
			p, e = openai.New(cfg)
		}
		if e != nil {
			t.Fatal("provider construction failed")
		}
		return p
	}
	for _, protocol := range []string{"chat", "responses", "anthropic"} {
		t.Run(protocol, func(t *testing.T) {
			ref, code := fixture(t)
			p := makeProvider(protocol, openai.OfficialDeepSeekVisionModel)
			r := provider.Request{MaxTokens: 2048, Messages: []provider.Message{
				{Role: provider.RoleUser, Content: "Return JSON with fields code (the printed letters and digits at the top of the image), left_color, right_color. Do not omit code. If unreadable say unreadable."},
				{Role: provider.RoleAssistant, ReasoningContent: "I will inspect the image using the tool.", ToolCalls: []provider.ToolCall{{ID: "image1", Name: "view_image", Arguments: `{"path":"fixture.png"}`}}},
				{Role: provider.RoleTool, ToolCallID: "image1", Name: "view_image", Content: "image loaded", Images: []string{ref}},
			}}
			out := collect(t, p, r)
			if !strings.Contains(strings.ReplaceAll(strings.ToUpper(out), " ", ""), code) || !strings.Contains(strings.ToLower(out), "red") || !strings.Contains(strings.ToLower(out), "blue") {
				t.Fatalf("image semantic verification failed: %s", out)
			}
		})
	}
	vp := makeProvider("chat", openai.OfficialDeepSeekVisionModel)
	for _, mode := range []string{"explicit", "auto"} {
		t.Run(mode, func(t *testing.T) {
			target := "deepseek/" + openai.OfficialDeepSeekVisionModel
			selected := 0
			c := imageinput.Config{Model: target, Resolve: func(string) (provider.Provider, error) { return vp, nil }, Select: func(current, mode string) (string, bool) { selected++; return target, true }}
			if mode == "auto" {
				c.Model = "auto"
			}
			svc := imageinput.New(c)
			for range 2 {
				ref, code := fixture(t)
				ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
				summary, e := svc.Understand(ctx, "deepseek/deepseek-v4-flash", []string{ref}, nil, event.Discard)
				cancel()
				if e != nil {
					t.Fatalf("summary failed (%T)", e)
				}
				text := collect(t, makeProvider("chat", "deepseek-v4-flash"), provider.Request{MaxTokens: 1024, Messages: []provider.Message{{Role: provider.RoleUser, Content: imageinput.AppendSummary("Return JSON with code, left_color, right_color from the supplied summary. Do not omit code.", summary)}}})
				if !strings.Contains(strings.ReplaceAll(strings.ToUpper(text), " ", ""), code) {
					t.Fatalf("summary relay failed: summary=%s answer=%s", summary.Summary, text)
				}
			}
			if mode == "auto" && selected != 2 {
				t.Fatal("auto selector was not used")
			}
		})
	}
}
