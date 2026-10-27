package serve

import (
	"bytes"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/BurntSushi/toml"

	"reasonix/internal/contract/config"
)

func sceneNumbersTEX(img []byte) []byte {
	var b bytes.Buffer
	b.WriteString("TEXV0005")
	b.WriteByte(0)
	b.WriteString("TEXI0001")
	b.WriteByte(0)
	b.Write(img)
	return b.Bytes()
}

func TestSceneImportBoundsNonFiniteLayerNumbers(t *testing.T) {
	zoomHome(t, "")
	png := sceneTestPNG(t)
	files := map[string][]byte{
		"scene.json": []byte(`{"general":{"orthogonalprojection":{"width":100,"height":50}},"objects":[
			{"image":"models/a.json","origin":"nan nan 0","size":"inf 50","scale":"infinity 1 1"},
			{"image":"models/b.json","origin":"1e300 -1e300 0","size":"1e300 1e300"},
			{"image":"models/c.json","origin":"25 25 0","size":"50 50","scale":"1e300 1e300 1"}
		]}`),
		"models/a.json":    []byte(`{"material":"materials/a.json"}`),
		"materials/a.json": []byte(`{"passes":[{"textures":["a"]}]}`),
		"materials/a.tex":  sceneNumbersTEX(png),
		"models/b.json":    []byte(`{"material":"materials/b.json"}`),
		"materials/b.json": []byte(`{"passes":[{"textures":["b"]}]}`),
		"materials/b.tex":  sceneNumbersTEX(png),
		"models/c.json":    []byte(`{"material":"materials/c.json"}`),
		"materials/c.json": []byte(`{"passes":[{"textures":["c"]}]}`),
		"materials/c.tex":  sceneNumbersTEX(png),
	}
	rec, view := sceneUpload(t, sceneTestProject(), sceneTestPackage(t, files), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload = %d %s", rec.Code, rec.Body)
	}
	if len(view.Wallpaper.Layers) != 3 {
		t.Fatalf("layers = %d, want 3", len(view.Wallpaper.Layers))
	}
	for i, layer := range view.Wallpaper.Layers {
		for _, value := range []float64{layer.X, layer.Y, layer.W, layer.H, layer.Alpha} {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				t.Errorf("layer %d has non-finite geometry: %+v", i, layer)
			}
		}
		if layer.X < -4 || layer.X > 4 || layer.Y < -4 || layer.Y > 4 || layer.W < 0 || layer.W > 4 || layer.H < 0 || layer.H > 4 {
			t.Errorf("layer %d geometry is outside the sane range: %+v", i, layer)
		}
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Desktop.Appearance.Wallpaper.Layers) != 3 {
		t.Fatalf("stored layers = %d, want 3", len(cfg.Desktop.Appearance.Wallpaper.Layers))
	}
	rendered := config.RenderTOMLForScope(cfg, config.RenderScopeUser)
	var decoded config.Config
	if _, err := toml.Decode(rendered, &decoded); err != nil {
		t.Fatalf("rendered config does not parse: %v\n%s", err, rendered)
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(rendered), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadForEditReadOnlyStrict(path); err != nil {
		t.Fatalf("rendered config does not load: %v\n%s", err, rendered)
	}
}
