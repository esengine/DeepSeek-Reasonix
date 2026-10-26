// appearance.go — the user's own size, type and picture, over any theme pack.
package serve

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/contract/config"
)

// wallpaperTypes is the allowlist. The extension is derived from the type
// rather than from a client-supplied name, so nothing decides its own path.
var wallpaperTypes = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
	"image/avif": ".avif",
	"image/gif":  ".gif",
	"video/mp4":  ".mp4",
	"video/webm": ".webm",
}

var audioTypes = map[string]string{
	"audio/mpeg": ".mp3",
	"audio/flac": ".flac",
}

func (s *Server) registerAppearanceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /appearance", s.appearance)
	mux.HandleFunc("POST /appearance", s.saveAppearance)
	mux.HandleFunc("POST /appearance/wallpaper", s.uploadWallpaper)
	mux.HandleFunc("POST /appearance/wallpaper/scene", s.uploadWallpaperScene)
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
	// The range Zoom is held to, so the page draws its slider from the same
	// bounds the save clamps to.
	ZoomRange zoomRangeView `json:"zoomRange"`
	ReadSize  float64       `json:"readSize,omitempty"`
	// Interface text alone, held to config.TextSizeMin..TextSizeMax on save.
	TextSize float64 `json:"textSize,omitempty"`
	FontUI   string  `json:"fontUi,omitempty"`
	FontMono string  `json:"fontMono,omitempty"`
	// standard | full — how far the transcript's prose runs.
	Width     string         `json:"width,omitempty"`
	Wallpaper *wallpaperView `json:"wallpaper,omitempty"`
}

type zoomRangeView struct {
	Min  float64 `json:"min"`
	Max  float64 `json:"max"`
	Step float64 `json:"step"`
}

type wallpaperLayerView struct {
	URL     string   `json:"url"`
	X       float64  `json:"x"`
	Y       float64  `json:"y"`
	W       float64  `json:"w"`
	H       float64  `json:"h"`
	Alpha   float64  `json:"alpha"`
	Effects []string `json:"effects,omitempty"`
}

type wallpaperAudioView struct {
	URL    string  `json:"url"`
	Volume float64 `json:"volume"`
	Loop   bool    `json:"loop"`
}

type wallpaperView struct {
	// URL carries the content hash in its name, so the bytes at an address
	// never change and the page needs no cache buster.
	URL string `json:"url"`
	// Kind is "image", "video" or "scene"; the kernel decides it from the
	// stored type so the page never infers a renderer from a name.
	Kind        string               `json:"kind"`
	Title       string               `json:"title,omitempty"`
	Audio       string               `json:"audio,omitempty"`
	Audios      []string             `json:"audios,omitempty"`
	AudioTracks []wallpaperAudioView `json:"audioTracks,omitempty"`
	Layers      []wallpaperLayerView `json:"layers,omitempty"`
	SceneWidth  float64              `json:"sceneWidth,omitempty"`
	SceneHeight float64              `json:"sceneHeight,omitempty"`
	Parallax    float64              `json:"parallax,omitempty"`
	Muted       bool                 `json:"muted,omitempty"`
	Volume      float64              `json:"volume"`
	Opacity     float64              `json:"opacity"`
	Dim         float64              `json:"dim"`
	FocusX      float64              `json:"focusX"`
	FocusY      float64              `json:"focusY"`
}

func appearanceDir() string {
	return filepath.Join(config.MemoryUserDir(), "appearance")
}

func wallpaperAudioNames(w config.WallpaperConfig) []string {
	names := make([]string, 0, len(w.Audios)+1)
	seen := map[string]bool{}
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		names = append(names, name)
	}
	add(w.Audio)
	for _, name := range w.Audios {
		add(name)
	}
	return names
}

func wallpaperLayerNames(w config.WallpaperConfig) []string {
	names := make([]string, 0, len(w.Layers)+1)
	seen := map[string]bool{}
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		names = append(names, name)
	}
	add(w.File)
	for _, layer := range w.Layers {
		add(layer.File)
	}
	return names
}

func wallpaperVolume(w config.WallpaperConfig) float64 {
	if w.Volume == nil {
		return 1
	}
	return clamp01(*w.Volume)
}

func viewOf(a config.AppearanceConfig, language, width string) appearanceView {
	out := appearanceView{
		Language:  language,
		Zoom:      config.ClampZoom(a.Zoom),
		ZoomRange: zoomRangeView{Min: config.ZoomMin, Max: config.ZoomMax, Step: config.ZoomStep},
		ReadSize:  a.ReadSize,
		TextSize:  config.ClampTextSize(a.TextSize),
		FontUI:    a.FontUI,
		FontMono:  a.FontMono,
		Width:     width,
	}
	if a.Wallpaper.File != "" {
		kind := wallpaperKind(a.Wallpaper.File)
		if a.Wallpaper.Kind == "scene" {
			kind = "scene"
		}
		out.Wallpaper = &wallpaperView{
			URL:         "/appearance/wallpaper/" + a.Wallpaper.File,
			Kind:        kind,
			Title:       a.Wallpaper.Title,
			SceneWidth:  a.Wallpaper.SceneWidth,
			SceneHeight: a.Wallpaper.SceneHeight,
			Parallax:    a.Wallpaper.Parallax,
			Muted:       a.Wallpaper.Muted,
			Volume:      wallpaperVolume(a.Wallpaper),
			Opacity:     a.Wallpaper.Opacity,
			Dim:         a.Wallpaper.Dim,
			FocusX:      a.Wallpaper.FocusX,
			FocusY:      a.Wallpaper.FocusY,
		}
		names := wallpaperAudioNames(a.Wallpaper)
		for i, name := range names {
			url := "/appearance/wallpaper/" + name
			out.Wallpaper.Audios = append(out.Wallpaper.Audios, url)
			volume := 1.0
			if i < len(a.Wallpaper.AudioVolumes) {
				volume = clamp01(a.Wallpaper.AudioVolumes[i])
			}
			loop := true
			if i < len(a.Wallpaper.AudioLoops) {
				loop = a.Wallpaper.AudioLoops[i]
			}
			out.Wallpaper.AudioTracks = append(out.Wallpaper.AudioTracks, wallpaperAudioView{URL: url, Volume: volume, Loop: loop})
		}
		if len(out.Wallpaper.Audios) > 0 {
			out.Wallpaper.Audio = out.Wallpaper.Audios[0]
		}
		for _, layer := range a.Wallpaper.Layers {
			if layer.File == "" {
				continue
			}
			out.Wallpaper.Layers = append(out.Wallpaper.Layers, wallpaperLayerView{
				URL:     "/appearance/wallpaper/" + layer.File,
				X:       layer.X,
				Y:       layer.Y,
				W:       layer.W,
				H:       layer.H,
				Alpha:   clamp01(layer.Alpha),
				Effects: layer.Effects,
			})
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
		Language string   `json:"language"`
		Zoom     float64  `json:"zoom"`
		ReadSize float64  `json:"readSize"`
		TextSize float64  `json:"textSize"`
		FontUI   string   `json:"fontUi"`
		FontMono string   `json:"fontMono"`
		Width    string   `json:"width"`
		Opacity  float64  `json:"opacity"`
		Dim      float64  `json:"dim"`
		FocusX   float64  `json:"focusX"`
		FocusY   float64  `json:"focusY"`
		Muted    bool     `json:"muted"`
		Volume   *float64 `json:"volume"`
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
	// The page posts back the clamped value it was shown, so one equal to what
	// the stored value reads as is the user leaving it alone.
	if body.Zoom != config.ClampZoom(a.Zoom) {
		a.Zoom = config.ClampZoom(body.Zoom)
	}
	a.ReadSize = clampOrZero(body.ReadSize, 10, 26)
	a.TextSize = config.ClampTextSize(body.TextSize)
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
	a.Wallpaper.Muted = body.Muted
	if body.Volume != nil {
		volume := clamp01(*body.Volume)
		a.Wallpaper.Volume = &volume
	}
	if err := edit.SaveTo(config.UserConfigPath()); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, viewOf(edit.Desktop.Appearance, edit.DesktopLanguage(), edit.DesktopConversationWidth()))
}

// uploadWallpaper takes the picture as the request body, not as JSON, so the
// kernel never holds the file: it is copied to disk as it arrives, under a name
// derived from what is in it. The type comes in the query because the body's
// own content type has to be the one csrfGuard admits for binary uploads.
func (s *Server) uploadWallpaper(w http.ResponseWriter, r *http.Request) {
	name, err := storeWallpaper(r.URL.Query().Get("mime"), r.Body)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	edit := config.LoadForEdit(config.UserConfigPath())
	paper := &edit.Desktop.Appearance.Wallpaper
	prevLayers := wallpaperLayerNames(*paper)
	prevAudios := wallpaperAudioNames(*paper)
	paper.File = name
	paper.Layers = nil
	paper.Kind = ""
	paper.Title = ""
	paper.Audio = ""
	paper.Audios = nil
	paper.AudioVolumes = nil
	paper.AudioLoops = nil
	paper.Parallax = 0
	paper.Muted = false
	paper.Volume = nil
	// First picture: give it settings it can be seen at. Replacing one keeps
	// whatever the user had already dialled in.
	if len(prevLayers) == 0 {
		paper.Opacity = 0.5
		paper.Dim = 0.55
		paper.FocusX = 0.5
		paper.FocusY = 0.5
	}
	if err := edit.SaveTo(config.UserConfigPath()); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	for _, old := range prevLayers {
		if old != name {
			_ = os.Remove(filepath.Join(appearanceDir(), old))
		}
	}
	for _, old := range prevAudios {
		_ = os.Remove(filepath.Join(appearanceDir(), old))
	}
	writeJSON(w, viewOf(edit.Desktop.Appearance, edit.DesktopLanguage(), edit.DesktopConversationWidth()))
}

func (s *Server) clearWallpaper(w http.ResponseWriter, r *http.Request) {
	edit := config.LoadForEdit(config.UserConfigPath())
	paper := edit.Desktop.Appearance.Wallpaper
	prevLayers := wallpaperLayerNames(paper)
	prevAudios := wallpaperAudioNames(paper)
	edit.Desktop.Appearance.Wallpaper = config.WallpaperConfig{}
	if err := edit.SaveTo(config.UserConfigPath()); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	for _, old := range prevLayers {
		_ = os.Remove(filepath.Join(appearanceDir(), old))
	}
	for _, old := range prevAudios {
		_ = os.Remove(filepath.Join(appearanceDir(), old))
	}
	w.WriteHeader(http.StatusNoContent)
}

// The name is content-addressed, so these bytes are immutable and cached hard.
// Only a name the config currently points at is served: the directory is not
// a static root, and a request must not be able to walk out of it.
func (s *Server) wallpaperAsset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	cfg, err := config.Load()
	if err != nil {
		notFound(w, "wallpaper", name)
		return
	}
	paper := cfg.Desktop.Appearance.Wallpaper
	allowed := paper.File == name
	for _, layer := range wallpaperLayerNames(paper) {
		if layer == name {
			allowed = true
		}
	}
	for _, audio := range wallpaperAudioNames(paper) {
		if audio == name {
			allowed = true
		}
	}
	if !allowed {
		notFound(w, "wallpaper", name)
		return
	}
	f, err := os.Open(filepath.Join(appearanceDir(), name))
	if err != nil {
		notFound(w, "wallpaper", name)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		notFound(w, "wallpaper", name)
		return
	}
	w.Header().Set("Content-Type", contentTypeOf(name))
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, name, info.ModTime(), f)
}

// storeWallpaper copies the picture to disk as it is read and names it for its
// content. The copy is made beside the final name and renamed into place, so an
// upload that breaks off leaves nothing behind. The only refusals left are the
// ones about what was sent: a type nobody allows, or nothing at all.
func storeWallpaper(mime string, r io.Reader) (string, error) {
	ext, ok := wallpaperTypes[strings.ToLower(strings.TrimSpace(mime))]
	if !ok {
		return "", refusal(http.StatusUnprocessableEntity, "wallpaper.unsupported_type", errors.New("unsupported wallpaper type"), nil)
	}
	dir := appearanceDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, hash), r)
	if err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if n == 0 {
		return "", refusal(http.StatusUnprocessableEntity, "wallpaper.empty", errors.New("the image is empty"), nil)
	}
	name := hex.EncodeToString(hash.Sum(nil)[:8]) + ext
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, name)); err != nil {
		return "", err
	}
	return name, nil
}

// wallpaperKind says whether a stored wallpaper is a picture or a video, by the
// type its name was derived from.
func wallpaperKind(name string) string {
	if strings.HasPrefix(contentTypeOf(name), "video/") {
		return "video"
	}
	return "image"
}

func writeAudio(mime string, raw []byte) (string, error) {
	ext, ok := audioTypes[strings.ToLower(strings.TrimSpace(mime))]
	if !ok {
		return "", refusal(http.StatusUnprocessableEntity, "wallpaper.audio_unsupported", errors.New("unsupported audio type"), nil)
	}
	if len(raw) == 0 {
		return "", refusal(http.StatusUnprocessableEntity, "wallpaper.empty", errors.New("the audio is empty"), nil)
	}
	if len(raw) > maxSceneAsset {
		return "", refusal(http.StatusUnprocessableEntity, "wallpaper.scene_too_large", fmt.Errorf("audio is larger than %d MB", maxSceneAsset>>20), map[string]any{"limit": maxSceneAsset >> 20})
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
	for mime, ext := range audioTypes {
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
