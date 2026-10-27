package serve

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"

	"reasonix/internal/contract/config"
)

// maxSceneAsset bounds the package and each extracted audio track. A scene
// package is a container of pictures and sound, not a second window's worth of
// data held in memory.
const maxSceneAsset = 32 << 20

func (s *Server) uploadWallpaperScene(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, (maxSceneAsset/3)*4*2+1024)
	var body struct {
		Project string `json:"project"`
		Scene   string `json:"scene"`
		Preview string `json:"preview"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		badBody(w)
		return
	}
	project, err := decodeScenePart(body.Project)
	if err != nil {
		refuse(w, http.StatusBadRequest, "wallpaper.scene_invalid", "the folder data was not base64", nil)
		return
	}
	scene, err := decodeScenePart(body.Scene)
	if err != nil {
		refuse(w, http.StatusBadRequest, "wallpaper.scene_invalid", "the folder data was not base64", nil)
		return
	}
	if len(scene) > maxSceneAsset {
		refuse(w, http.StatusUnprocessableEntity, "wallpaper.scene_too_large", "the scene package is too large", map[string]any{"limit": maxSceneAsset >> 20})
		return
	}
	preview, err := decodeScenePart(body.Preview)
	if err != nil {
		refuse(w, http.StatusBadRequest, "wallpaper.scene_invalid", "the folder data was not base64", nil)
		return
	}
	if len(preview) > maxSceneAsset {
		refuse(w, http.StatusUnprocessableEntity, "wallpaper.scene_too_large", "the preview is too large", map[string]any{"limit": maxSceneAsset >> 20})
		return
	}

	got, err := importSceneWallpaper(project, scene, preview)
	if err != nil {
		code := "wallpaper.scene_invalid"
		switch {
		case errors.Is(err, errSceneUnsupported):
			code = "wallpaper.scene_unsupported"
		case errors.Is(err, errSceneNoImage):
			code = "wallpaper.scene_no_image"
		}
		refuse(w, http.StatusUnprocessableEntity, code, err.Error(), nil)
		return
	}

	layerNames, layers, err := storeSceneLayers(got.Layers)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	if len(layers) == 0 {
		refuse(w, http.StatusUnprocessableEntity, "wallpaper.scene_no_image", "the scene contains no image", nil)
		return
	}

	audioNames, audioVolumes, audioLoops, err := storeSceneAudio(got.Audios)
	if err != nil {
		removeWallpaperFiles(layerNames)
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}

	edit := config.LoadForEdit(config.UserConfigPath())
	paper := &edit.Desktop.Appearance.Wallpaper
	prevLayers := wallpaperLayerNames(*paper)
	prevAudios := wallpaperAudioNames(*paper)
	paper.File = layerNames[0]
	paper.Layers = layers
	paper.SceneWidth = got.SceneWidth
	paper.SceneHeight = got.SceneHeight
	paper.Kind = "scene"
	paper.Title = got.Title
	paper.Audio = ""
	paper.Audios = audioNames
	paper.AudioVolumes = audioVolumes
	paper.AudioLoops = audioLoops
	if len(audioNames) > 0 {
		paper.Audio = audioNames[0]
	}
	paper.Parallax = got.Parallax
	paper.Muted = true
	if paper.Volume == nil {
		volume := 1.0
		paper.Volume = &volume
	}
	if len(prevLayers) == 0 {
		paper.Opacity = 0.5
		paper.Dim = 0.55
		paper.FocusX = 0.5
		paper.FocusY = 0.5
	}
	if err := edit.SaveTo(config.UserConfigPath()); err != nil {
		removeWallpaperFiles(layerNames)
		removeWallpaperFiles(audioNames)
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	for _, old := range prevLayers {
		if !slices.Contains(layerNames, old) {
			_ = os.Remove(filepath.Join(appearanceDir(), old))
		}
	}
	for _, old := range prevAudios {
		if !slices.Contains(audioNames, old) {
			_ = os.Remove(filepath.Join(appearanceDir(), old))
		}
	}
	writeJSON(w, viewOf(edit.Desktop.Appearance, edit.DesktopLanguage(), edit.DesktopConversationWidth()))
}

func removeWallpaperFiles(names []string) {
	for _, name := range names {
		_ = os.Remove(filepath.Join(appearanceDir(), name))
	}
}

func storeSceneLayers(layers []sceneLayer) ([]string, []config.WallpaperLayer, error) {
	names := make([]string, 0, len(layers))
	stored := make([]config.WallpaperLayer, 0, len(layers))
	for _, layer := range layers {
		name, err := storeWallpaper(layer.MIME, bytes.NewReader(layer.Image))
		if err != nil {
			removeWallpaperFiles(names)
			return nil, nil, err
		}
		names = append(names, name)
		stored = append(stored, config.WallpaperLayer{
			File:    name,
			X:       layer.X,
			Y:       layer.Y,
			W:       layer.W,
			H:       layer.H,
			Alpha:   layer.Alpha,
			Effects: layer.Effects,
		})
	}
	return names, stored, nil
}

func storeSceneAudio(audios []sceneAudio) ([]string, []float64, []bool, error) {
	names := make([]string, 0, len(audios))
	volumes := make([]float64, 0, len(audios))
	loops := make([]bool, 0, len(audios))
	for _, audio := range audios {
		name, err := writeAudio(audio.MIME, audio.Data)
		if err != nil {
			removeWallpaperFiles(names)
			return nil, nil, nil, err
		}
		names = append(names, name)
		volumes = append(volumes, audio.Volume)
		loops = append(loops, audio.Loop)
	}
	return names, volumes, loops, nil
}

func decodeScenePart(value string) ([]byte, error) {
	if value == "" {
		return nil, nil
	}
	return base64.StdEncoding.DecodeString(value)
}
