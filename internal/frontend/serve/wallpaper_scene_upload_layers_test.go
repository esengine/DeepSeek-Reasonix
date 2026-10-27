package serve

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path"
	"testing"

	"reasonix/internal/contract/config"
)

func sceneLayerTEX(img []byte) []byte {
	var b bytes.Buffer
	b.WriteString("TEXV0005")
	b.WriteByte(0)
	b.WriteString("TEXI0001")
	b.WriteByte(0)
	b.Write(img)
	return b.Bytes()
}

func TestSceneUploadStoresEveryLayer(t *testing.T) {
	zoomHome(t, "")
	png := sceneTestPNG(t)
	jpeg := sceneTestJPEG(t)
	files := map[string][]byte{
		"scene.json": []byte(`{"general":{"orthogonalprojection":{"width":100,"height":50}},"objects":[
			{"image":"models/a.json","origin":"25 25 0","size":"50 50"},
			{"image":"models/b.json","origin":"75 25 0","size":"50 50"}
		]}`),
		"models/a.json":    []byte(`{"material":"materials/a.json"}`),
		"materials/a.json": []byte(`{"passes":[{"textures":["a"]}]}`),
		"materials/a.tex":  sceneLayerTEX(png),
		"models/b.json":    []byte(`{"material":"materials/b.json"}`),
		"materials/b.json": []byte(`{"passes":[{"textures":["b"]}]}`),
		"materials/b.tex":  sceneLayerTEX(jpeg),
	}
	rec, view := sceneUpload(t, sceneTestProject(), sceneTestPackage(t, files), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload = %d %s", rec.Code, rec.Body)
	}
	if view.Wallpaper == nil || len(view.Wallpaper.Layers) != 2 {
		t.Fatalf("layers = %+v, want two stored layers", view.Wallpaper)
	}
	if view.Wallpaper.SceneWidth != 100 || view.Wallpaper.SceneHeight != 50 {
		t.Errorf("scene = %gx%g, want 100x50", view.Wallpaper.SceneWidth, view.Wallpaper.SceneHeight)
	}
	if view.Wallpaper.Layers[0].URL == view.Wallpaper.Layers[1].URL {
		t.Error("two different layers share one content address")
	}
	for _, layer := range view.Wallpaper.Layers {
		req := httptest.NewRequest(http.MethodGet, layer.URL, nil)
		req.SetPathValue("name", path.Base(layer.URL))
		got := httptest.NewRecorder()
		(&Server{}).wallpaperAsset(got, req)
		if got.Code != http.StatusOK {
			t.Errorf("GET %s = %d", layer.URL, got.Code)
		}
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Desktop.Appearance.Wallpaper.Layers) != 2 {
		t.Errorf("stored layers = %d, want 2", len(cfg.Desktop.Appearance.Wallpaper.Layers))
	}
}
