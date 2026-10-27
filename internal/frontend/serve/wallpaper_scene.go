package serve

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"math"
	"path"
	"sort"
	"strconv"
	"strings"
)

// sceneWallpaper is the part of a Wallpaper Engine scene the page can show
// without its shader runtime: the backdrop, its soundtrack, and how far the
// camera moves with the pointer.
type sceneAudio struct {
	Data   []byte
	MIME   string
	Volume float64
	Loop   bool
}

type sceneLayer struct {
	Image   []byte
	MIME    string
	X, Y    float64
	W, H    float64
	Alpha   float64
	Effects []string
}

type sceneWallpaper struct {
	Image       []byte
	ImageMIME   string
	Layers      []sceneLayer
	SceneWidth  float64
	SceneHeight float64
	Title       string
	Audios      []sceneAudio
	Parallax    float64
}

var (
	errSceneInvalid     = errors.New("invalid Wallpaper Engine scene")
	errSceneUnsupported = errors.New("unsupported Wallpaper Engine wallpaper type")
	errSceneNoImage     = errors.New("the scene contains no image")
)

type sceneProject struct {
	Title string `json:"title"`
	Type  string `json:"type"`
	File  string `json:"file"`
}

type sceneDocument struct {
	General struct {
		CameraParallax               bool    `json:"cameraparallax"`
		CameraParallaxAmount         float64 `json:"cameraparallaxamount"`
		CameraParallaxMouseInfluence float64 `json:"cameraparallaxmouseinfluence"`
		OrthogonalProjection         struct {
			Width  float64 `json:"width"`
			Height float64 `json:"height"`
		} `json:"orthogonalprojection"`
	} `json:"general"`
	Objects []struct {
		Image   string   `json:"image"`
		Sound   []string `json:"sound"`
		Origin  string   `json:"origin"`
		Size    string   `json:"size"`
		Scale   string   `json:"scale"`
		Alpha   *float64 `json:"alpha"`
		Visible *bool    `json:"visible"`
		Effects []struct {
			File string `json:"file"`
		} `json:"effects"`
		Volume       float64 `json:"volume"`
		PlaybackMode string  `json:"playbackmode"`
		StartSilent  bool    `json:"startsilent"`
	} `json:"objects"`
}

// importSceneWallpaper reads the files a Wallpaper Engine folder keeps beside
// project.json. Shaders and particles are deliberately ignored: this is the
// still backdrop, not a second scene engine.
func importSceneWallpaper(project, pkg, preview []byte) (sceneWallpaper, error) {
	var meta sceneProject
	if err := json.Unmarshal(project, &meta); err != nil {
		return sceneWallpaper{}, fmt.Errorf("%w: project.json: %w", errSceneInvalid, err)
	}
	if !strings.EqualFold(strings.TrimSpace(meta.Type), "scene") {
		return sceneWallpaper{}, fmt.Errorf("%w: type %q", errSceneUnsupported, meta.Type)
	}
	title := strings.TrimSpace(meta.Title)
	if title == "" {
		title = "Wallpaper Engine scene"
	}
	if len(title) > 200 {
		title = title[:200]
	}

	files, err := parseWallpaperPackage(pkg)
	if err != nil {
		return sceneWallpaper{}, fmt.Errorf("%w: %w", errSceneInvalid, err)
	}
	scenePath := cleanAssetPath(meta.File)
	if scenePath == "" {
		scenePath = "scene.json"
	}
	rawScene, ok := files[scenePath]
	if !ok {
		return sceneWallpaper{}, fmt.Errorf("%w: scene file %q is missing", errSceneInvalid, scenePath)
	}
	var scene sceneDocument
	if err := json.Unmarshal(rawScene, &scene); err != nil {
		return sceneWallpaper{}, fmt.Errorf("%w: %s: %w", errSceneInvalid, scenePath, err)
	}

	out := sceneWallpaper{Title: title}
	layers, sceneWidth, sceneHeight := sceneLayers(files, scene)
	if len(layers) == 0 && len(preview) > 0 {
		if mime := imageMIME(preview); mime != "" {
			layers = []sceneLayer{{Image: preview, MIME: mime, W: 1, H: 1, Alpha: 1}}
			sceneWidth, sceneHeight = 1, 1
		}
	}
	if len(layers) == 0 {
		return sceneWallpaper{}, errSceneNoImage
	}
	out.Layers = layers
	out.SceneWidth, out.SceneHeight = sceneWidth, sceneHeight
	out.Image = layers[0].Image
	out.ImageMIME = layers[0].MIME
	out.Audios = sceneSounds(files, scene)
	out.Parallax = 0.02
	if scene.General.CameraParallax {
		out.Parallax = scene.General.CameraParallaxAmount
		if out.Parallax <= 0 {
			out.Parallax = 0.02
		}
		if out.Parallax > 0.2 {
			out.Parallax = 0.2
		}
	}
	return out, nil
}

func sceneObjectImage(files map[string][]byte, image string) ([]byte, bool) {
	imagePath := cleanAssetPath(image)
	if imagePath == "" {
		return nil, false
	}
	rawModel, ok := files[imagePath]
	if !ok {
		return nil, false
	}
	var model struct {
		Material string `json:"material"`
	}
	if json.Unmarshal(rawModel, &model) != nil {
		return nil, false
	}
	materialPath := cleanAssetPath(model.Material)
	if materialPath == "" {
		return nil, false
	}
	raw, ok := files[materialPath]
	if !ok {
		return nil, false
	}
	var material struct {
		Passes []struct {
			Textures []string `json:"textures"`
		} `json:"passes"`
	}
	if json.Unmarshal(raw, &material) != nil {
		return nil, false
	}
	for _, pass := range material.Passes {
		for _, texture := range pass.Textures {
			if imageBytes, ok := sceneTexture(files, materialPath, texture); ok {
				return imageBytes, true
			}
		}
	}
	return nil, false
}

const (
	maxSceneDimension = 1 << 20
	maxLayerExtent    = 4.0
)

func sceneLayers(files map[string][]byte, scene sceneDocument) ([]sceneLayer, float64, float64) {
	sceneWidth := saneSceneDimension(scene.General.OrthogonalProjection.Width)
	sceneHeight := saneSceneDimension(scene.General.OrthogonalProjection.Height)
	layers := make([]sceneLayer, 0, len(scene.Objects))
	for _, object := range scene.Objects {
		if object.Visible != nil && !*object.Visible {
			continue
		}
		imageBytes, ok := sceneObjectImage(files, object.Image)
		if !ok {
			continue
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(imageBytes))
		if err != nil {
			continue
		}
		imageW, imageH := float64(cfg.Width), float64(cfg.Height)
		sizeX, sizeY := parsePair(object.Size, imageW, imageH)
		scaleX, scaleY := parsePair(object.Scale, 1, 1)
		if scaleX < 0 {
			scaleX = -scaleX
		}
		if scaleY < 0 {
			scaleY = -scaleY
		}
		if sceneWidth <= 0 {
			sceneWidth = saneSceneDimension(imageW)
		}
		if sceneHeight <= 0 {
			sceneHeight = saneSceneDimension(imageH)
		}
		originX, originY := parsePair(object.Origin, sceneWidth/2, sceneHeight/2)
		alpha := 1.0
		if object.Alpha != nil {
			alpha = *object.Alpha
		}
		effects := make([]string, 0, len(object.Effects))
		for _, effect := range object.Effects {
			if file := cleanAssetPath(effect.File); file != "" {
				effects = append(effects, file)
			}
		}
		x := (originX - sizeX*scaleX/2) / sceneWidth
		y := (originY - sizeY*scaleY/2) / sceneHeight
		w := sizeX * scaleX / sceneWidth
		h := sizeY * scaleY / sceneHeight
		layers = append(layers, sceneLayer{
			Image:   imageBytes,
			MIME:    imageMIME(imageBytes),
			X:       boundLayer(x, -maxLayerExtent, maxLayerExtent),
			Y:       boundLayer(y, -maxLayerExtent, maxLayerExtent),
			W:       boundLayer(w, 0, maxLayerExtent),
			H:       boundLayer(h, 0, maxLayerExtent),
			Alpha:   clampAlpha(alpha),
			Effects: effects,
		})
		if len(layers) >= 16 {
			break
		}
	}
	if sceneWidth <= 0 || sceneHeight <= 0 {
		sceneWidth, sceneHeight = 1, 1
	}
	return layers, sceneWidth, sceneHeight
}

func parsePair(value string, dx, dy float64) (float64, float64) {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return dx, dy
	}
	parse := func(s string, fallback float64) float64 {
		n, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return fallback
		}
		return n
	}
	x := parse(fields[0], dx)
	y := x
	if len(fields) > 1 {
		y = parse(fields[1], dy)
	}
	return x, y
}

func saneSceneDimension(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 || value > maxSceneDimension {
		return 0
	}
	return value
}

func boundLayer(value, lo, hi float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return lo
	}
	if value < lo {
		return lo
	}
	if value > hi {
		return hi
	}
	return value
}

func clampAlpha(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 1
	}
	return clamp01(value)
}

func sceneTexture(files map[string][]byte, materialPath, texture string) ([]byte, bool) {
	texture = cleanAssetPath(texture)
	if texture == "" {
		return nil, false
	}
	candidates := []string{texture, texture + ".tex", path.Join("materials", texture+".tex"), path.Join(path.Dir(materialPath), texture+".tex")}
	for _, candidate := range candidates {
		candidate = cleanAssetPath(candidate)
		if candidate == "" {
			continue
		}
		if raw, ok := files[candidate]; ok {
			if imageBytes, _, err := imageFromTEX(raw); err == nil {
				return imageBytes, true
			}
		}
	}
	return nil, false
}

const maxSceneAudioTracks = 8

func sceneSounds(files map[string][]byte, scene sceneDocument) []sceneAudio {
	seen := map[string]bool{}
	out := make([]sceneAudio, 0, 2)
	add := func(name string, volume float64, loop bool) {
		name = cleanAssetPath(name)
		if name == "" || seen[name] || len(out) >= maxSceneAudioTracks {
			return
		}
		raw, ok := files[name]
		mime := audioMIME(name)
		if !ok || mime == "" || len(raw) == 0 {
			return
		}
		if math.IsNaN(volume) || math.IsInf(volume, 0) || volume <= 0 {
			volume = 1
		} else {
			volume = clamp01(volume)
		}
		seen[name] = true
		out = append(out, sceneAudio{Data: raw, MIME: mime, Volume: volume, Loop: loop})
	}
	for _, object := range scene.Objects {
		for _, sound := range object.Sound {
			add(sound, object.Volume, !strings.EqualFold(object.PlaybackMode, "once"))
		}
	}
	if len(out) == 0 {
		names := make([]string, 0, len(files))
		for name := range files {
			if audioMIME(name) != "" {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		for _, name := range names {
			add(name, 1, true)
		}
	}
	return out
}

// parseWallpaperPackage reads Wallpaper Engine's PKG container. The table is
// the only thing interpreted; entry data stays in memory and is never unpacked
// to a path a name could choose.
func parseWallpaperPackage(raw []byte) (map[string][]byte, error) {
	if len(raw) < 16 || string(raw[4:8]) != "PKGV" {
		return nil, errors.New("not a Wallpaper Engine package")
	}
	count := binary.LittleEndian.Uint32(raw[12:16])
	if count > 10000 {
		return nil, fmt.Errorf("package declares %d entries", count)
	}
	off := 16
	type entry struct {
		name   string
		offset uint32
		length uint32
	}
	entries := make([]entry, 0, count)
	for range count {
		if off+4 > len(raw) {
			return nil, errors.New("package table is truncated")
		}
		nameLen := binary.LittleEndian.Uint32(raw[off : off+4])
		off += 4
		if nameLen > 4096 || off+int(nameLen)+8 > len(raw) {
			return nil, errors.New("package entry name is out of bounds")
		}
		name := string(raw[off : off+int(nameLen)])
		off += int(nameLen)
		entryOffset := binary.LittleEndian.Uint32(raw[off : off+4])
		entryLength := binary.LittleEndian.Uint32(raw[off+4 : off+8])
		off += 8
		entries = append(entries, entry{name: name, offset: entryOffset, length: entryLength})
	}
	data := raw[off:]
	files := make(map[string][]byte, len(entries))
	for _, item := range entries {
		start := uint64(item.offset)
		end := start + uint64(item.length)
		if end > uint64(len(data)) {
			return nil, fmt.Errorf("package entry %q is out of bounds", item.name)
		}
		files[item.name] = data[start:end]
	}
	return files, nil
}

var (
	pngSignature  = []byte("\x89PNG\r\n\x1a\n")
	jpegSignature = []byte{0xff, 0xd8, 0xff}
)

// maxSceneImagePixels bounds the frame jpeg.Decode may allocate for a scene's
// backdrop. A header can declare a huge frame without carrying any image data.
const maxSceneImagePixels = int64(64 << 20)

// imageFromTEX pulls the PNG or JPEG a TEX container may carry. Masks and
// depth maps use compressed GPU formats and are not needed by the still
// backdrop.
func imageFromTEX(raw []byte) ([]byte, string, error) {
	if start := bytes.Index(raw, pngSignature); start >= 0 {
		rest := raw[start:]
		end := bytes.Index(rest, []byte("IEND"))
		if end >= 0 && end+8 <= len(rest) {
			out := rest[:end+8]
			if mime := imageMIME(out); mime != "" {
				return out, mime, nil
			}
		}
	}
	if out, mime, ok := dxtFromTEX(raw); ok {
		return out, mime, nil
	}
	if start := bytes.Index(raw, jpegSignature); start >= 0 {
		rest := raw[start:]
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(rest))
		if err != nil {
			return nil, "", errSceneNoImage
		}
		if int64(cfg.Width)*int64(cfg.Height) > maxSceneImagePixels {
			return nil, "", fmt.Errorf("%w: JPEG %dx%d exceeds %d pixels", errSceneInvalid, cfg.Width, cfg.Height, maxSceneImagePixels)
		}
		// A TEX may hold several mip JPEGs back to back, and EXIF can carry a
		// thumbnail with its own EOI. Decode the first image so the marker
		// scan follows the real structure instead of guessing at "FF D9".
		reader := bytes.NewReader(rest)
		if _, err := jpeg.Decode(reader); err == nil {
			consumed := len(rest) - reader.Len()
			out := rest[:consumed]
			if mime := imageMIME(out); mime != "" {
				return out, mime, nil
			}
		}
	}
	return nil, "", errSceneNoImage
}

func imageMIME(raw []byte) string {
	_, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return ""
	}
	switch format {
	case "png":
		return "image/png"
	case "jpeg":
		return "image/jpeg"
	default:
		return ""
	}
}

func audioMIME(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".mp3":
		return "audio/mpeg"
	case ".flac":
		return "audio/flac"
	default:
		return ""
	}
}

// cleanAssetPath keeps package names relative and inside the package.
func cleanAssetPath(name string) string {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	name = strings.TrimPrefix(name, "/")
	name = path.Clean(name)
	if name == "." || name == "" || strings.HasPrefix(name, "../") || name == ".." {
		return ""
	}
	return name
}
