package meshparse

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// ---------------------------------------------------------------------------
// glTF (.gltf) parser — pure Go, zero dependencies (docs/MODELLING_OVERHAUL.md
// §6-B extension). Supports the minimal mesh subset every glTF must contain:
//   buffer → bufferView → accessor (POSITION VEC3 float / indices SCALAR
//   uint16/uint32) with embedded base64 data URIs or external .bin files.
// ---------------------------------------------------------------------------

// gltfJSON mirrors the glTF 2.0 JSON structure we consume.
type gltfJSON struct {
	Asset struct {
		Version string `json:"version"`
	} `json:"asset"`
	Buffers []struct {
		URI        string `json:"uri"`
		ByteLength int    `json:"byteLength"`
	} `json:"buffers"`
	BufferViews []gltfJSONBufferView `json:"bufferViews"`
	Accessors  []gltfJSONAccessor    `json:"accessors"`
	Meshes []struct {
		Primitives []struct {
			Attributes map[string]int `json:"attributes"`
			Indices    *int           `json:"indices"`
		} `json:"primitives"`
	} `json:"meshes"`
}

// ParseGLTF parses a .gltf file into a normalized Mesh (first primitive of the
// first mesh with POSITION + optional indices).
func ParseGLTF(path string) (*Mesh, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var g gltfJSON
	if err := json.Unmarshal(data, &g); err != nil {
		return nil, fmt.Errorf("gltf: invalid JSON: %w", err)
	}
	buffers, err := loadGLTFBuffers(path, g)
	if err != nil {
		return nil, err
	}
	return parseGLTFJSON(&g, buffers, "gltf")
}

// loadGLTFBuffers resolves every buffer (embedded base64 data URI or external
// .bin relative to the glTF file).
func loadGLTFBuffers(path string, g gltfJSON) ([][]byte, error) {
	buffers := make([][]byte, len(g.Buffers))
	for i, b := range g.Buffers {
		if b.URI == "" {
			return nil, fmt.Errorf("gltf buffer %d: no uri (binary glTF must use a BIN chunk)", i)
		}
		raw, err := loadGLTFBuffer(path, b.URI)
		if err != nil {
			return nil, fmt.Errorf("gltf buffer %d: %w", i, err)
		}
		if len(raw) < b.ByteLength {
			return nil, fmt.Errorf("gltf buffer %d: %d bytes, need %d", i, len(raw), b.ByteLength)
		}
		buffers[i] = raw
	}
	return buffers, nil
}

// parseGLTFJSON decodes a glTF JSON document into a normalized Mesh given
// resolved buffers. Shared by .gltf and .glb paths.
func parseGLTFJSON(g *gltfJSON, buffers [][]byte, format string) (*Mesh, error) {
	if g.Asset.Version == "" {
		return nil, errors.New("gltf: missing asset.version")
	}
	if len(g.Meshes) == 0 {
		return nil, errors.New("gltf: no meshes")
	}
	if len(g.Meshes[0].Primitives) == 0 {
		return nil, errors.New("gltf: mesh has no primitives")
	}
	prim := g.Meshes[0].Primitives[0]
	posAcc, ok := prim.Attributes["POSITION"]
	if !ok {
		return nil, errors.New("gltf: primitive has no POSITION attribute")
	}
	if posAcc < 0 || posAcc >= len(g.Accessors) {
		return nil, fmt.Errorf("gltf: POSITION accessor %d out of range (have %d)", posAcc, len(g.Accessors))
	}
	pos := g.Accessors[posAcc]

	m := &Mesh{Format: format}

	// POSITION accessor → verts.
	if pos.Type != "VEC3" {
		return nil, fmt.Errorf("gltf POSITION type %q, want VEC3", pos.Type)
	}
	posData, err := sliceAccessor(buffers, g.BufferViews, pos)
	if err != nil {
		return nil, err
	}
	for i := 0; i < pos.Count; i++ {
		x, y, z, ok := decodeVec3(posData, i, pos.ComponentType, pos.Count)
		if !ok {
			return nil, fmt.Errorf("gltf POSITION accessor decode failed at %d", i)
		}
		m.Verts = append(m.Verts, Vec3{x, y, z})
	}

	// Indices accessor (optional; otherwise sequential tris).
	if prim.Indices != nil && *prim.Indices >= 0 && *prim.Indices < len(g.Accessors) {
		idx := g.Accessors[*prim.Indices]
		if idx.Type != "SCALAR" {
			return nil, fmt.Errorf("gltf indices type %q, want SCALAR", idx.Type)
		}
		idxData, err := sliceAccessor(buffers, g.BufferViews, idx)
		if err != nil {
			return nil, err
		}
		for i := 0; i+2 < idx.Count; i += 3 {
			a, ok1 := decodeScalar(idxData, i, idx.ComponentType)
			b, ok2 := decodeScalar(idxData, i+1, idx.ComponentType)
			c, ok3 := decodeScalar(idxData, i+2, idx.ComponentType)
			if !ok1 || !ok2 || !ok3 {
				return nil, errors.New("gltf indices decode failed")
			}
			m.Faces = append(m.Faces, Face{Verts: []int{a, b, c}})
		}
	} else {
		for i := 0; i+2 < len(m.Verts); i += 3 {
			m.Faces = append(m.Faces, Face{Verts: []int{i, i + 1, i + 2}})
		}
	}
	if len(m.Verts) == 0 {
		return nil, errors.New("gltf: no vertices")
	}
	return m, nil
}

// sliceAccessor returns the accessor's raw byte slice (respecting bufferView
// offset/length) for decoding.
func sliceAccessor(buffers [][]byte, views []gltfJSONBufferView, a gltfJSONAccessor) ([]byte, error) {
	if a.BufferView < 0 || a.BufferView >= len(views) {
		return nil, fmt.Errorf("gltf: accessor bufferView %d out of range", a.BufferView)
	}
	bv := views[a.BufferView]
	if bv.Buffer < 0 || bv.Buffer >= len(buffers) {
		return nil, fmt.Errorf("gltf: bufferView buffer %d out of range", bv.Buffer)
	}
	// Check both offsets non-negative BEFORE adding — the sum can overflow
	// (wrap negative) even when each operand is valid, which would defeat the
	// bounds checks below and panic on a negative slice index.
	if bv.ByteOffset < 0 || a.ByteOffset < 0 {
		return nil, fmt.Errorf("gltf: negative byte offsets (bufferView %d, accessor %d)", bv.ByteOffset, a.ByteOffset)
	}
	if a.ByteOffset > int(^uint(0)>>1)-bv.ByteOffset {
		return nil, fmt.Errorf("gltf: accessor byte offset overflows (bufferView %d + accessor %d)", bv.ByteOffset, a.ByteOffset)
	}
	start := bv.ByteOffset + a.ByteOffset
	l := a.length()
	if l < 0 || start > int(^uint(0)>>1)-l {
		return nil, fmt.Errorf("gltf: accessor slice overflows (start %d, len %d)", start, l)
	}
	end := start + l
	if end > len(buffers[bv.Buffer]) {
		return nil, fmt.Errorf("gltf: accessor slice [%d:%d] exceeds buffer (%d)", start, end, len(buffers[bv.Buffer]))
	}
	return buffers[bv.Buffer][start:end], nil
}

// decodeVec3 decodes element i of a VEC3 accessor (normalized ints supported).
func decodeVec3(data []byte, i int, comp int, count int) (float64, float64, float64, bool) {
	off := i * compSize(comp) * 3
	if off+compSize(comp)*3 > len(data) {
		return 0, 0, 0, false
	}
	xs, ok1 := decodeComponent(data, off, comp, count)
	ys, ok2 := decodeComponent(data, off+compSize(comp), comp, count)
	zs, ok3 := decodeComponent(data, off+2*compSize(comp), comp, count)
	return xs, ys, zs, ok1 && ok2 && ok3
}

func decodeScalar(data []byte, i int, comp int) (int, bool) {
	off := i * compSize(comp)
	if off+compSize(comp) > len(data) {
		return 0, false
	}
	// float components for indices are unusual; accept them via cast.
	switch comp {
	case 5121: // ubyte
		return int(data[off]), true
	case 5123: // ushort
		return int(binary.LittleEndian.Uint16(data[off:])), true
	case 5125: // uint
		return int(binary.LittleEndian.Uint32(data[off:])), true
	case 5126: // float
		return int(math.Round(float64(math.Float32frombits(binary.LittleEndian.Uint32(data[off:]))))), true
	}
	return 0, false
}

// decodeComponent decodes one numeric component with normalization.
func decodeComponent(data []byte, off, comp, count int) (float64, bool) {
	if off < 0 || off+compSize(comp) > len(data) {
		return 0, false
	}
	switch comp {
	case 5120: // byte
		return float64(int8(data[off])), true
	case 5121: // ubyte
		return float64(data[off]), true
	case 5122: // short
		return float64(int16(binary.LittleEndian.Uint16(data[off:]))), true
	case 5123: // ushort
		return float64(binary.LittleEndian.Uint16(data[off:])), true
	case 5125: // uint
		return float64(binary.LittleEndian.Uint32(data[off:])), true
	case 5126: // float
		return float64(math.Float32frombits(binary.LittleEndian.Uint32(data[off:]))), true
	}
	return 0, false
}

func compSize(comp int) int {
	switch comp {
	case 5120, 5121:
		return 1
	case 5122, 5123:
		return 2
	case 5125, 5126:
		return 4
	}
	return 0
}

// loadGLTFBuffer resolves a buffer URI: embedded base64 data URI or external
// file relative to the glTF. External files must resolve inside the glTF's own
// directory (no traversal / absolute paths) and are size-capped.
func loadGLTFBuffer(gltfPath, uri string) ([]byte, error) {
	if strings.HasPrefix(uri, "data:") {
		comma := strings.IndexByte(uri, ',')
		if comma < 0 {
			return nil, errors.New("gltf: malformed data URI")
		}
		meta := uri[:comma]
		if !strings.Contains(meta, "base64") {
			return nil, errors.New("gltf: non-base64 data URI unsupported")
		}
		dec, err := base64.StdEncoding.DecodeString(uri[comma+1:])
		if err != nil {
			return nil, fmt.Errorf("gltf: bad base64 buffer: %w", err)
		}
		if len(dec) > maxBufferBytes {
			return nil, fmt.Errorf("gltf: embedded buffer %d bytes exceeds limit %d", len(dec), maxBufferBytes)
		}
		return dec, nil
	}
	if filepath.IsAbs(uri) || strings.Contains(uri, "..") {
		return nil, fmt.Errorf("gltf: external buffer path %q must be relative and inside the glTF directory", uri)
	}
	binPath := filepath.Join(filepath.Dir(gltfPath), uri)
	// Resolve symlinks and require the result stays inside the glTF directory.
	dir, err := filepath.EvalSymlinks(filepath.Dir(gltfPath))
	if err != nil {
		dir = filepath.Dir(gltfPath)
	}
	resolved, err := filepath.EvalSymlinks(binPath)
	if err != nil {
		// No path in the error: the resolved absolute path could leak server
		// layout to the LLM/user via the tool layer.
		return nil, fmt.Errorf("gltf: external buffer %q not found or not readable", uri)
	}
	if !strings.HasPrefix(filepath.Clean(resolved), filepath.Clean(dir)+string(filepath.Separator)) {
		return nil, fmt.Errorf("gltf: external buffer %q escapes the glTF directory", uri)
	}
	fi, err := os.Stat(binPath)
	if err != nil {
		// No path in the error (TOCTOU/stat race must not leak the resolved
		// server path either) — same style as the EvalSymlinks branch above.
		return nil, fmt.Errorf("gltf: external buffer %q not found or not readable", uri)
	}
	if fi.Size() > maxBufferBytes {
		return nil, fmt.Errorf("gltf: external buffer %q is %d bytes, exceeds limit %d", uri, fi.Size(), maxBufferBytes)
	}
	return os.ReadFile(binPath)
}

// gltfJSONBufferView / gltfJSONAccessor are aliases for the embedded structs.
type gltfJSONBufferView struct {
	Buffer     int `json:"buffer"`
	ByteOffset int `json:"byteOffset"`
	ByteLength int `json:"byteLength"`
}

type gltfJSONAccessor struct {
	BufferView    int `json:"bufferView"`
	ComponentType int `json:"componentType"`
	Count         int `json:"count"`
	Type          string `json:"type"`
	ByteOffset    int `json:"byteOffset"`
	ByteLength    int `json:"byteLength"`
}

// length returns the accessor's byte length (explicit byteLength, or derived
// from count × component size × type arity).
func (a gltfJSONAccessor) length() int {
	if a.ByteLength > 0 {
		return a.ByteLength
	}
	if a.ByteLength < 0 {
		return -1
	}
	arity := 1
	switch a.Type {
	case "VEC2":
		arity = 2
	case "VEC3":
		arity = 3
	case "VEC4", "MAT2":
		arity = 4
	case "MAT3":
		arity = 9
	case "MAT4":
		arity = 16
	}
	return a.Count * compSize(a.ComponentType) * arity
}
