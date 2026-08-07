package meshparse

import (
	"encoding/base64"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// buildGLTFCube builds a minimal glTF (embedded base64 buffer) with an 8-vert
// cube + 12 triangle indices (same cube as cubeOBJ).
func buildGLTFCube(t *testing.T, path string) {
	t.Helper()
	verts := []float32{
		0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0,
		0, 0, 1, 1, 0, 1, 1, 1, 1, 0, 1, 1,
	}
	// 12 triangles × 3 uint16 indices.
	quads := [][]int{
		{0, 1, 2, 3}, {4, 7, 6, 5}, {0, 4, 5, 1},
		{1, 5, 6, 2}, {2, 6, 7, 3}, {3, 7, 4, 0},
	}
	var idx []uint16
	for _, q := range quads {
		idx = append(idx, uint16(q[0]), uint16(q[1]), uint16(q[2]), uint16(q[0]), uint16(q[2]), uint16(q[3]))
	}
	buf := make([]byte, 0, len(verts)*4+len(idx)*2)
	for _, v := range verts {
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], math.Float32bits(v))
		buf = append(buf, b[:]...)
	}
	for _, i := range idx {
		var b [2]byte
		binary.LittleEndian.PutUint16(b[:], i)
		buf = append(buf, b[:]...)
	}
	uri := "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString(buf)
	gltf := `{
"asset":{"version":"2.0"},
"buffers":[{"uri":"` + uri + `","byteLength":` + itoa(len(buf)) + `}],
"bufferViews":[
  {"buffer":0,"byteOffset":0,"byteLength":` + itoa(len(verts)*4) + `},
  {"buffer":0,"byteOffset":` + itoa(len(verts)*4) + `,"byteLength":` + itoa(len(idx)*2) + `}
],
"accessors":[
  {"bufferView":0,"componentType":5126,"count":8,"type":"VEC3"},
  {"bufferView":1,"componentType":5123,"count":36,"type":"SCALAR"}
],
"meshes":[{"primitives":[{"attributes":{"POSITION":0},"indices":1}]}]
}`
	if err := os.WriteFile(path, []byte(gltf), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseGLTFEmbeddedCube(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cube.gltf")
	buildGLTFCube(t, p)
	m, err := ParseGLTF(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Verts) != 8 || len(m.Faces) != 12 {
		t.Fatalf("gltf cube: %d verts %d faces, want 8/12", len(m.Verts), len(m.Faces))
	}
	d := Analyze(m)
	if !d.Manifold || !d.Water || d.Comps != 1 {
		t.Errorf("gltf cube should be manifold+watertight+1 comp: %+v", d)
	}
	if d.Tris != 12 {
		t.Errorf("gltf cube tris = %d, want 12", d.Tris)
	}
}

func TestParseGLTFExternalBin(t *testing.T) {
	dir := t.TempDir()
	// Minimal triangle via external .bin file.
	verts := []float32{0, 0, 0, 1, 0, 0, 0, 1, 0}
	buf := make([]byte, 0, len(verts)*4)
	for _, v := range verts {
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], math.Float32bits(v))
		buf = append(buf, b[:]...)
	}
	if err := os.WriteFile(filepath.Join(dir, "tri.bin"), buf, 0o644); err != nil {
		t.Fatal(err)
	}
	gltf := `{
"asset":{"version":"2.0"},
"buffers":[{"uri":"tri.bin","byteLength":36}],
"bufferViews":[{"buffer":0,"byteOffset":0,"byteLength":36}],
"accessors":[{"bufferView":0,"componentType":5126,"count":3,"type":"VEC3"}],
"meshes":[{"primitives":[{"attributes":{"POSITION":0}}]}]
}`
	p := filepath.Join(dir, "tri.gltf")
	if err := os.WriteFile(p, []byte(gltf), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := ParseGLTF(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Verts) != 3 || len(m.Faces) != 1 {
		t.Fatalf("gltf tri: %d verts %d faces, want 3/1", len(m.Verts), len(m.Faces))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}
