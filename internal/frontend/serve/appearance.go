// appearance.go — the user's own size, type and picture, over any theme pack.
package serve

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/contract/config"
)

// maxWallpaperUpload bounds what a window will hold in memory to decode and
// write. A desktop background past this is a photo nobody downscaled.
const maxWallpaperUpload = 12 << 20

// wallpaperTypes is the allowlist. The extension is derived from the type
// rather than from a client-supplied name, so nothing decides its own path.
var wallpaperTypes = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
	"image/avif": ".avif",
	"image/gif":  ".gif",
}

func (s *Server) registerAppearanceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /appearance", s.appearance)
	mux.HandleFunc("POST /appearance", s.saveAppearance)
	mux.HandleFunc("POST /appearance/wallpaper", s.uploadWallpaper)
	mux.HandleFunc("DELETE /appearance/wallpaper", s.clearWallpaper)
	mux.HandleFunc("GET /appearance/wallpaper/{name}", s.wallpaperAsset)
}

// Which families the machine can actually render is not answered here: the
// page can ask its own font system with document.fonts.check(), which needs no
// permission and is right about what it will really draw with.
type appearanceView struct {
	// Interface language: "zh", "en", or "" to follow the machine. It is not
	// the language the model answers in — that follows each message you write.
	Language string  `json:"language,omitempty"`
	Zoom     float64 `json:"zoom,omitempty"`
	ReadSize float64 `json:"readSize,omitempty"`
	FontUI   string  `json:"fontUi,omitempty"`
	FontMono string  `json:"fontMono,omitempty"`
	// standard | full — how far the transcript's prose runs.
	Width     string         `json:"width,omitempty"`
	Wallpaper *wallpaperView `json:"wallpaper,omitempty"`
}

type wallpaperView struct {
	// URL carries the content hash in its name, so the bytes at an address
	// never change and the page needs no cache buster.
	URL     string  `json:"url"`
	Opacity float64 `json:"opacity"`
	Dim     float64 `json:"dim"`
	FocusX  float64 `json:"focusX"`
	FocusY  float64 `json:"focusY"`
}

func appearanceDir() string {
	return filepath.Join(config.MemoryUserDir(), "appearance")
}

func viewOf(a config.AppearanceConfig, language, width string) appearanceView {
	out := appearanceView{Language: language, Zoom: a.Zoom, ReadSize: a.ReadSize, FontUI: a.FontUI, FontMono: a.FontMono, Width: width}
	if a.Wallpaper.File != "" {
		out.Wallpaper = &wallpaperView{
			URL:     "/appearance/wallpaper/" + a.Wallpaper.File,
			Opacity: a.Wallpaper.Opacity,
			Dim:     a.Wallpaper.Dim,
			FocusX:  a.Wallpaper.FocusX,
			FocusY:  a.Wallpaper.FocusY,
		}
	}
	return out
}

func (s *Server) appearance(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, viewOf(cfg.Desktop.Appearance, cfg.DesktopLanguage(), cfg.DesktopConversationWidth()))
}

// Sizes are clamped rather than rejected: a value out of range is a slider
// that got away, and refusing the whole save would lose the rest of it.
func (s *Server) saveAppearance(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Language string  `json:"language"`
		Zoom     float64 `json:"zoom"`
		ReadSize float64 `json:"readSize"`
		FontUI   string  `json:"fontUi"`
		FontMono string  `json:"fontMono"`
		Width    string  `json:"width"`
		Opacity  float64 `json:"opacity"`
		Dim      float64 `json:"dim"`
		FocusX   float64 `json:"focusX"`
		FocusY   float64 `json:"focusY"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		badBody(w)
		return
	}
	edit := config.LoadForEdit(config.UserConfigPath())
	// "" is a real answer here — it means follow the machine — so an unknown
	// value is dropped to that rather than kept.
	switch strings.ToLower(strings.TrimSpace(body.Language)) {
	case "zh", "en":
		edit.Desktop.Language = strings.ToLower(strings.TrimSpace(body.Language))
	default:
		edit.Desktop.Language = ""
	}
	a := &edit.Desktop.Appearance
	a.Zoom = clampOrZero(body.Zoom, 0.7, 1.8)
	a.ReadSize = clampOrZero(body.ReadSize, 10, 26)
	a.FontUI = sanitizeFamily(body.FontUI)
	a.FontMono = sanitizeFamily(body.FontMono)
	// Anything but the one named alternative is the default, the same way the
	// language field above resolves an answer it does not know.
	if err := edit.SetDesktopConversationWidth(body.Width); err != nil {
		_ = edit.SetDesktopConversationWidth("standard")
	}
	a.Wallpaper.Opacity = clamp01(body.Opacity)
	a.Wallpaper.Dim = clamp01(body.Dim)
	a.Wallpaper.FocusX = clamp01(body.FocusX)
	a.Wallpaper.FocusY = clamp01(body.FocusY)
	if err := edit.SaveTo(config.UserConfigPath()); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, viewOf(edit.Desktop.Appearance, edit.DesktopLanguage(), edit.DesktopConversationWidth()))
}

func (s *Server) uploadWallpaper(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, (maxWallpaperUpload/3)*4+1024)
	var body struct {
		Mime string `json:"mime"`
		Data string `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		badBody(w)
		return
	}
	raw, err := base64.StdEncoding.DecodeString(body.Data)
	if err != nil {
		refuse(w, http.StatusBadRequest, "wallpaper.not_base64", "the image data was not base64", nil)
		return
	}
	name, err := writeWallpaper(body.Mime, raw)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	edit := config.LoadForEdit(config.UserConfigPath())
	prev := edit.Desktop.Appearance.Wallpaper.File
	edit.Desktop.Appearance.Wallpaper.File = name
	// First picture: give it settings it can be seen at. Replacing one keeps
	// whatever the user had already dialled in.
	if prev == "" {
		edit.Desktop.Appearance.Wallpaper.Opacity = 0.5
		edit.Desktop.Appearance.Wallpaper.Dim = 0.55
		edit.Desktop.Appearance.Wallpaper.FocusX = 0.5
		edit.Desktop.Appearance.Wallpaper.FocusY = 0.5
	}
	if err := edit.SaveTo(config.UserConfigPath()); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if prev != "" && prev != name {
		_ = os.Remove(filepath.Join(appearanceDir(), prev))
	}
	writeJSON(w, viewOf(edit.Desktop.Appearance, edit.DesktopLanguage(), edit.DesktopConversationWidth()))
}

func (s *Server) clearWallpaper(w http.ResponseWriter, r *http.Request) {
	edit := config.LoadForEdit(config.UserConfigPath())
	prev := edit.Desktop.Appearance.Wallpaper.File
	edit.Desktop.Appearance.Wallpaper = config.WallpaperConfig{}
	if err := edit.SaveTo(config.UserConfigPath()); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if prev != "" {
		_ = os.Remove(filepath.Join(appearanceDir(), prev))
	}
	w.WriteHeader(http.StatusNoContent)
}

// The name is content-addressed, so these bytes are immutable and cached hard.
// Only a name the config currently points at is served: the directory is not
// a static root, and a request must not be able to walk out of it.
func (s *Server) wallpaperAsset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	cfg, err := config.Load()
	if err != nil || cfg.Desktop.Appearance.Wallpaper.File != name {
		notFound(w, "wallpaper", name)
		return
	}
	raw, err := os.ReadFile(filepath.Join(appearanceDir(), name))
	if err != nil {
		notFound(w, "wallpaper", name)
		return
	}
	w.Header().Set("Content-Type", contentTypeOf(name))
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	_, _ = w.Write(raw)
}

// writeWallpaper stores the bytes under a name derived from what is in them.
func writeWallpaper(mime string, raw []byte) (string, error) {
	ext, ok := wallpaperTypes[strings.ToLower(strings.TrimSpace(mime))]
	if !ok {
		return "", refusal(http.StatusUnprocessableEntity, "wallpaper.unsupported_type", errors.New("unsupported image type"), nil)
	}
	if len(raw) == 0 {
		return "", refusal(http.StatusUnprocessableEntity, "wallpaper.empty", errors.New("the image is empty"), nil)
	}
	if len(raw) > maxWallpaperUpload {
		return "", refusal(http.StatusUnprocessableEntity, "wallpaper.too_large", fmt.Errorf("image is larger than %d MB", maxWallpaperUpload>>20), map[string]any{"limit": maxWallpaperUpload >> 20})
	}
	sum := sha256.Sum256(raw)
	name := hex.EncodeToString(sum[:8]) + ext
	dir := appearanceDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, name), raw, 0o644); err != nil {
		return "", err
	}
	return name, nil
}

func contentTypeOf(name string) string {
	for mime, ext := range wallpaperTypes {
		if strings.HasSuffix(name, ext) {
			return mime
		}
	}
	return "application/octet-stream"
}

// A family list reaches a stylesheet, so the characters that could end the
// declaration and start another one are what must not survive.
func sanitizeFamily(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) > 200 {
		s = s[:200]
	}
	if strings.ContainsAny(s, ";{}<>()\\\n\r") {
		return ""
	}
	return s
}

func clamp01(v float64) float64 { return clamp(v, 0, 1) }

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// clampOrZero keeps "unset" distinct from "set to the minimum": zero means the
// stylesheet decides, which is not the same as the smallest the slider offers.
func clampOrZero(v, lo, hi float64) float64 {
	if v <= 0 {
		return 0
	}
	return clamp(v, lo, hi)
}
