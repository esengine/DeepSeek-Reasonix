package serve

import (
	"bytes"
	"testing"
)

func TestImportSceneWallpaperKeepsLayersAndTrackDetails(t *testing.T) {
	png := sceneTestPNG(t)
	tex := append([]byte("TEXV0005\x00TEXI0001\x00"), png...)
	files := map[string][]byte{
		"scene.json": []byte(`{"general":{"orthogonalprojection":{"width":100,"height":50}},"objects":[
			{"image":"models/a.json","origin":"50 25 0","size":"100 50","alpha":0.5,"effects":[{"file":"effects/glow/effect.json"}]},
			{"image":"models/b.json","origin":"25 25 0","size":"20 10","scale":"2 2 1"},
			{"sound":["sounds/loop.mp3"],"volume":0.4,"playbackmode":"loop"},
			{"sound":["sounds/once.mp3"],"volume":0.8,"playbackmode":"once"}
		]}`),
		"models/a.json":    []byte(`{"material":"materials/a.json"}`),
		"materials/a.json": []byte(`{"passes":[{"textures":["a"]}]}`),
		"materials/a.tex":  tex,
		"models/b.json":    []byte(`{"material":"materials/b.json"}`),
		"materials/b.json": []byte(`{"passes":[{"textures":["b"]}]}`),
		"materials/b.tex":  tex,
		"sounds/loop.mp3":  []byte("loop"),
		"sounds/once.mp3":  []byte("once"),
	}

	got, err := importSceneWallpaper(sceneTestProject(), sceneTestPackage(t, files), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Layers) != 2 {
		t.Fatalf("layers = %d, want 2", len(got.Layers))
	}
	first, second := got.Layers[0], got.Layers[1]
	if first.X != 0 || first.Y != 0 || first.W != 1 || first.H != 1 || first.Alpha != 0.5 {
		t.Errorf("first layer = %+v, want the full backdrop at alpha 0.5", first)
	}
	if len(first.Effects) != 1 || first.Effects[0] != "effects/glow/effect.json" {
		t.Errorf("first layer effects = %v", first.Effects)
	}
	if second.X != 0.05 || second.Y != 0.3 || second.W != 0.4 || second.H != 0.4 || second.Alpha != 1 {
		t.Errorf("second layer = %+v, want the scaled inset layer", second)
	}
	if len(got.Audios) != 2 {
		t.Fatalf("audios = %d, want 2", len(got.Audios))
	}
	if got.Audios[0].Volume != 0.4 || !got.Audios[0].Loop {
		t.Errorf("first audio = %+v, want volume 0.4 looping", got.Audios[0])
	}
	if got.Audios[1].Volume != 0.8 || got.Audios[1].Loop {
		t.Errorf("second audio = %+v, want volume 0.8 once", got.Audios[1])
	}
	if !bytes.Equal(second.Image, png) {
		t.Error("second layer image differs from its texture")
	}
}
