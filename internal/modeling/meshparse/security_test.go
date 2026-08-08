package meshparse

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Security regression tests: hostile input must fail cleanly, never panic
// and never read/write outside the intended paths. (fixes from security review)

func TestParseObjOutOfRangeIndexFails(t *testing.T) {
	// "f 999999 ..." — vertex index beyond the declared verts. Previously
	// accepted and panicked later in analyzers/operators.
	src := "v 0 0 0\nv 1 0 0\nv 0 1 0\nf 1 2 999999\n"
	if _, err := ParseOBJ(strings.NewReader(src)); err != nil {
		t.Fatalf("ParseOBJ should accept raw parse (validation is at Parse entry): %v", err)
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.obj")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(p); err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("Parse with out-of-range index: want out-of-range error, got %v", err)
	}
}

func TestParseObjNegativeResolvesWithinBounds(t *testing.T) {
	// Negative indices are relative and must stay in range after resolution.
	src := "v 0 0 0\nv 1 0 0\nv 0 1 0\nf -1 -2 -3\n"
	if _, err := ParseOBJ(strings.NewReader(src)); err != nil {
		t.Fatalf("valid negative indices rejected: %v", err)
	}
}

func TestGltfTraversalURIRejected(t *testing.T) {
	dir := t.TempDir()
	// glTF referencing ../../etc/passwd as an external buffer.
	gltf := `{"asset":{"version":"2.0"},"buffers":[{"uri":"..%s","byteLength":4}],"bufferViews":[{"buffer":0,"byteOffset":0,"byteLength":4}],"accessors":[],"meshes":[{"primitives":[]}]}`
	// Use a literal traversal (no %s in the JSON file).
	bad := `{"asset":{"version":"2.0"},"buffers":[{"uri":"../../secret.bin","byteLength":4}],"meshes":[]}`
	p := filepath.Join(dir, "evil.gltf")
	if err := os.WriteFile(p, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(p); err == nil || !strings.Contains(err.Error(), "escapes") && !strings.Contains(err.Error(), "relative") {
		t.Fatalf("traversal URI: want rejection, got %v", err)
	}
	_ = gltf
}

func TestGltfAccessorOutOfRange(t *testing.T) {
	// POSITION accessor index 7 with only 1 accessor → must error, not panic.
	// Go through the real Parse path (embedded base64 POSITION data) so the
	// accessor-range guard is actually exercised.
	dir := t.TempDir()
	pos := make([]byte, 12) // 1 float32 VEC3, zeroed
	enc := base64.StdEncoding.EncodeToString(pos)
	bad := `{"asset":{"version":"2.0"},"buffers":[{"uri":"data:application/octet-stream;base64,` + enc + `","byteLength":12}],"bufferViews":[{"buffer":0,"byteOffset":0,"byteLength":12}],"accessors":[{"bufferView":0,"componentType":5126,"count":1,"type":"VEC3"}],"meshes":[{"primitives":[{"attributes":{"POSITION":7}}]}]}`
	p := filepath.Join(dir, "bad.gltf")
	if err := os.WriteFile(p, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(p); err == nil || !strings.Contains(err.Error(), "POSITION accessor") {
		t.Fatalf("POSITION accessor 7 out of range: want accessor error, got %v", err)
	}
}

func TestVoxChunkTooLargeRejected(t *testing.T) {
	// Header declaring a 1 GiB content chunk → must error, not allocate.
	hdr := make([]byte, 12)
	hdr[0], hdr[1], hdr[2], hdr[3] = 'S', 'I', 'Z', 'E'
	// content size = 1<<30 (attacker-declared)
	hdr[4] = 0x00
	hdr[5] = 0x00
	hdr[6] = 0x00
	hdr[7] = 0x40
	if _, _, _, err := readVoxChunk(strings.NewReader(string(hdr))); err == nil {
		t.Fatalf("1 GiB chunk: want size rejection, got nil")
	}
}

func TestGltfNegativeByteOffsetRejected(t *testing.T) {
	bad := `{"asset":{"version":"2.0"},"buffers":[{"byteLength":12}],"bufferViews":[{"buffer":0,"byteOffset":-5,"byteLength":12}],"accessors":[{"bufferView":0,"byteOffset":0,"componentType":5126,"count":1,"type":"VEC3"}],"meshes":[{"primitives":[{"attributes":{"POSITION":0}}]}]}`
	// parseGLTFJSON path requires buffers to be loaded; simulate via sliceAccessor directly.
	if _, err := sliceAccessor([][]byte{make([]byte, 12)}, []gltfJSONBufferView{{Buffer: 0, ByteOffset: -5, ByteLength: 12}}, gltfJSONAccessor{BufferView: 0}); err == nil {
		t.Fatalf("negative bufferView offset: want error")
	}
	_ = bad
}

func TestGltfEmptyPrimitivesRejected(t *testing.T) {
	dir := t.TempDir()
	bad := `{"asset":{"version":"2.0"},"meshes":[{"primitives":[]}]}`
	p := filepath.Join(dir, "empty.gltf")
	if err := os.WriteFile(p, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(p); err == nil || !strings.Contains(err.Error(), "no primitives") {
		t.Fatalf("empty primitives: want error, got %v", err)
	}
}

func TestSliceAccessorOffsetOverflowRejected(t *testing.T) {
	// Two huge non-negative offsets whose sum wraps negative — must be caught
	// before the negative slice index panics.
	buf := make([]byte, 64)
	big := int(int64(1) << 62) // 2^62 — two of these sum past MaxInt64
	_, err := sliceAccessor([][]byte{buf},
		[]gltfJSONBufferView{{Buffer: 0, ByteOffset: big, ByteLength: 8}},
		gltfJSONAccessor{BufferView: 0, ByteOffset: big})
	if err == nil || !strings.Contains(err.Error(), "overflows") {
		t.Fatalf("offset overflow: want overflow error, got %v", err)
	}
}

func TestParsePLYShortHeaderLineRejected(t *testing.T) {
	// A bare "format" line (no value) previously indexed f[1] → panic.
	src := "ply\nformat\nend_header\n"
	if _, err := ParsePLY(strings.NewReader(src)); err == nil || !strings.Contains(err.Error(), "missing value") {
		t.Fatalf("short format line: want error, got %v", err)
	}
	src2 := "ply\nformat ascii 1.0\nelement\nend_header\n"
	if _, err := ParsePLY(strings.NewReader(src2)); err == nil || !strings.Contains(err.Error(), "missing name/count") {
		t.Fatalf("short element line: want error, got %v", err)
	}
}
