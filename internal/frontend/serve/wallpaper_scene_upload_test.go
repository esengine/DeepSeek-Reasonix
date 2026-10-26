package serve

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
)

func sceneUpload(t *testing.T, project, pkg, preview []byte) (*httptest.ResponseRecorder, appearanceView) {
	t.Helper()
	body, err := json.Marshal(map[string]string{
		"project": base64.StdEncoding.EncodeToString(project),
		"scene":   base64.StdEncoding.EncodeToString(pkg),
		"preview": base64.StdEncoding.EncodeToString(preview),
	})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/appearance/wallpaper/scene", bytes.NewReader(body))
	(&Server{}).uploadWallpaperScene(rec, req)
	var view appearanceView
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
			t.Fatal(err)
		}
	}
	return rec, view
}

func TestSceneUploadStoresBackdropAudioAndParallax(t *testing.T) {
	zoomHome(t, "")
	pkg := sceneTestPackage(t, sceneTestFiles(append([]byte("TEXV0005\x00TEXI0001\x00"), sceneTestPNG(t)...)))

	rec, view := sceneUpload(t, sceneTestProject(), pkg, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload = %d %s", rec.Code, rec.Body)
	}
	if view.Wallpaper == nil {
		t.Fatal("the upload returned no wallpaper")
	}
	if view.Wallpaper.Kind != "scene" || view.Wallpaper.Title != "Test Scene" {
		t.Errorf("wallpaper = %+v, want a scene titled Test Scene", view.Wallpaper)
	}
	if view.Wallpaper.Audio == "" || view.Wallpaper.Parallax != 0.03 {
		t.Errorf("wallpaper audio/parallax = %q/%v, want an mp3 and 0.03", view.Wallpaper.Audio, view.Wallpaper.Parallax)
	}
	if view.Wallpaper.Volume != 1 {
		t.Errorf("wallpaper volume = %v, want the scene default of 1", view.Wallpaper.Volume)
	}
	if !view.Wallpaper.Muted {
		t.Error("a newly imported scene must start muted")
	}
	if len(view.Wallpaper.Layers) != 1 || view.Wallpaper.Layers[0].URL == "" {
		t.Errorf("layers = %+v, want one stored layer", view.Wallpaper.Layers)
	}
	if len(view.Wallpaper.AudioTracks) != 1 || view.Wallpaper.AudioTracks[0].Volume != 1 || !view.Wallpaper.AudioTracks[0].Loop {
		t.Errorf("audio tracks = %+v, want one looping track at volume 1", view.Wallpaper.AudioTracks)
	}

	raw, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`kind = "scene"`, `title = "Test Scene"`, `parallax = 0.03`, `volume = 1`, `audio_volumes = [1]`, `audio_loops = [true]`, `[[desktop.appearance.wallpaper.layers]]`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("stored config lacks %q:\n%s", want, raw)
		}
	}

	for _, asset := range []struct {
		url  string
		mime string
		body []byte
	}{
		{view.Wallpaper.URL, "image/png", sceneTestPNG(t)},
		{view.Wallpaper.Audio, "audio/mpeg", []byte("ID3-test-audio")},
	} {
		req := httptest.NewRequest(http.MethodGet, asset.url, nil)
		req.SetPathValue("name", path.Base(asset.url))
		got := httptest.NewRecorder()
		(&Server{}).wallpaperAsset(got, req)
		if got.Code != http.StatusOK {
			t.Errorf("GET %s = %d %s", asset.url, got.Code, got.Body)
			continue
		}
		if contentType := got.Header().Get("Content-Type"); contentType != asset.mime {
			t.Errorf("GET %s content type = %q, want %q", asset.url, contentType, asset.mime)
		}
		if nosniff := got.Header().Get("X-Content-Type-Options"); nosniff != "nosniff" {
			t.Errorf("GET %s X-Content-Type-Options = %q, want nosniff", asset.url, nosniff)
		}
		if !bytes.Equal(got.Body.Bytes(), asset.body) {
			t.Errorf("GET %s returned different bytes", asset.url)
		}
	}
}

func TestSceneUploadRejectsAnotherWallpaperType(t *testing.T) {
	zoomHome(t, "")
	rec, _ := sceneUpload(t, []byte(`{"title":"clip","type":"video","file":"scene.json"}`), sceneTestPackage(t, sceneTestFiles(nil)), nil)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "wallpaper.scene_unsupported") {
		t.Fatalf("video upload = %d %s, want scene_unsupported", rec.Code, rec.Body)
	}
}

func TestSceneUploadNeedsAnImage(t *testing.T) {
	zoomHome(t, "")
	rec, _ := sceneUpload(t, sceneTestProject(), sceneTestPackage(t, sceneTestFiles([]byte("TEXV0005\x00TEXI0001\x00 no image"))), nil)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "wallpaper.scene_no_image") {
		t.Fatalf("imageless scene = %d %s, want scene_no_image", rec.Code, rec.Body)
	}
}

func TestMutedSceneSurvivesSave(t *testing.T) {
	zoomHome(t, "")
	pkg := sceneTestPackage(t, sceneTestFiles(append([]byte("TEXV0005\x00TEXI0001\x00"), sceneTestPNG(t)...)))
	rec, _ := sceneUpload(t, sceneTestProject(), pkg, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload = %d %s", rec.Code, rec.Body)
	}
	got := zoomSave(t, map[string]any{"muted": true})
	if got.Wallpaper == nil || !got.Wallpaper.Muted {
		t.Fatalf("saved muted = %+v, want a muted scene", got.Wallpaper)
	}
}

func TestSavedVolumeIsClamped(t *testing.T) {
	zoomHome(t, "")
	pkg := sceneTestPackage(t, sceneTestFiles(append([]byte("TEXV0005\x00TEXI0001\x00"), sceneTestPNG(t)...)))
	rec, _ := sceneUpload(t, sceneTestProject(), pkg, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload = %d %s", rec.Code, rec.Body)
	}
	for _, c := range []struct{ sent, want float64 }{{2, 1}, {-1, 0}, {0.45, 0.45}} {
		got := zoomSave(t, map[string]any{"volume": c.sent})
		if got.Wallpaper == nil || got.Wallpaper.Volume != c.want {
			t.Errorf("volume %v saved as %+v, want %v", c.sent, got.Wallpaper, c.want)
		}
	}
}
