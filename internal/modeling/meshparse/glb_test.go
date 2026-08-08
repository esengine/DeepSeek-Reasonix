package meshparse

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// buildGLBCube writes a binary .glb containing the same 8-vert cube + 36
// indices as buildGLTFCube, with the geometry in the BIN chunk.
func buildGLBCube(t *testing.T, path string) {
	t.Helper()
	verts := []float32{
		0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0,
		0, 0, 1, 1, 0, 1, 1, 1, 1, 0, 1, 1,
	}
	quads := [][]int{
		{0, 1, 2, 3}, {4, 7, 6, 5}, {0, 4, 5, 1},
		{1, 5, 6, 2}, {2, 6, 7, 3}, {3, 7, 4, 0},
	}
	var idx []uint16
	for _, q := range quads {
		idx = append(idx, uint16(q[0]), uint16(q[1]), uint16(q[2]), uint16(q[0]), uint16(q[2]), uint16(q[3]))
	}
	bin := make([]byte, 0, len(verts)*4+len(idx)*2)
	for _, v := range verts {
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], math.Float32bits(v))
		bin = append(bin, b[:]...)
	}
	for _, i := range idx {
		var b [2]byte
		binary.LittleEndian.PutUint16(b[:], i)
		bin = append(bin, b[:]...)
	}
	gltf := map[string]any{
		"asset": map[string]any{"version": "2.0"},
		"buffers": []any{
			map[string]any{"byteLength": len(bin)},
		},
		"bufferViews": []any{
			map[string]any{"buffer": 0, "byteOffset": 0, "byteLength": len(verts) * 4},
			map[string]any{"buffer": 0, "byteOffset": len(verts) * 4, "byteLength": len(idx) * 2},
		},
		"accessors": []any{
			map[string]any{"bufferView": 0, "componentType": 5126, "count": 8, "type": "VEC3"},
			map[string]any{"bufferView": 1, "componentType": 5123, "count": 36, "type": "SCALAR"},
		},
		"meshes": []any{
			map[string]any{"primitives": []any{
				map[string]any{"attributes": map[string]any{"POSITION": 0}, "indices": 1},
			}},
		},
	}
	jsonBytes, _ := json.Marshal(gltf)
	// pad JSON to 4-byte alignment.
	for len(jsonBytes)%4 != 0 {
		jsonBytes = append(jsonBytes, ' ')
	}

	chunk := func(typ uint32, data []byte) []byte {
		out := make([]byte, 8+len(data))
		binary.LittleEndian.PutUint32(out[0:4], uint32(len(data)))
		binary.LittleEndian.PutUint32(out[4:8], typ)
		copy(out[8:], data)
		return out
	}
	body := append(chunk(glbChunkJSON, jsonBytes), chunk(glbChunkBIN, bin)...)
	total := glbHeaderSize + len(body)
	f := make([]byte, total)
	binary.LittleEndian.PutUint32(f[0:4], glbMagic)
	binary.LittleEndian.PutUint32(f[4:8], 2)
	binary.LittleEndian.PutUint32(f[8:12], uint32(total))
	copy(f[glbHeaderSize:], body)
	if err := os.WriteFile(path, f, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseGLBCube(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cube.glb")
	buildGLBCube(t, p)
	m, err := ParseGLB(p)
	if err != nil {
		t.Fatal(err)
	}
	if m.Format != "glb" {
		t.Errorf("format = %q, want glb", m.Format)
	}
	if len(m.Verts) != 8 || len(m.Faces) != 12 {
		t.Fatalf("glb cube: %d verts %d faces, want 8/12", len(m.Verts), len(m.Faces))
	}
	d := Analyze(m)
	if !d.Manifold || !d.Water || d.Comps != 1 {
		t.Errorf("glb cube should be manifold+watertight+1 comp: %+v", d)
	}
}

func TestParseGLBBadMagic(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.glb")
	if err := os.WriteFile(p, []byte("NOTGLB!"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseGLB(p); err == nil {
		t.Error("expected error for bad magic")
	}
}
