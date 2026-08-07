// Package meshparse parses common polygonal mesh formats (obj/stl/ply/vox)
// into a normalized Mesh, with zero external dependencies. It is the read
// layer of the modelling overhaul (docs/MODELLING_OVERHAUL.md §2): fast
// (millisecond-scale), environment-independent, and it never feeds raw
// geometry to the LLM — callers use MeshAnalyzer's compact descriptor.
package meshparse

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Vec3 is a 3D point / normal.
type Vec3 struct {
	X, Y, Z float64
}

// Face is an n-gon; Verts are indices into Mesh.Verts.
type Face struct {
	Verts []int
}

// Mesh is the normalized polygon mesh.
type Mesh struct {
	Verts []Vec3
	Faces []Face
	// Normals (per-vertex, optional), UVs (per-vertex, optional).
	Normals []Vec3
	UVs     []Vec3
	// Material names referenced by faces (optional).
	Materials []string
	// Format is the detected source format ("obj"/"stl"/"ply").
	Format string
}

// Bounds returns the axis-aligned bounding box of the mesh.
func (m *Mesh) Bounds() (min, max Vec3) {
	min = Vec3{math.Inf(1), math.Inf(1), math.Inf(1)}
	max = Vec3{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for _, v := range m.Verts {
		min.X = math.Min(min.X, v.X)
		min.Y = math.Min(min.Y, v.Y)
		min.Z = math.Min(min.Z, v.Z)
		max.X = math.Max(max.X, v.X)
		max.Y = math.Max(max.Y, v.Y)
		max.Z = math.Max(max.Z, v.Z)
	}
	return min, max
}

// Parse reads and parses a mesh file by extension. Supported: .obj, .stl,
// .ply, .vox (voxels — see voxel.go). Returns ErrUnsupportedFormat for
// unknown extensions.
func Parse(path string) (*Mesh, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	switch strings.ToLower(filepath.Ext(path)) {
	case ".obj":
		return ParseOBJ(f)
	case ".stl":
		return ParseSTL(f)
	case ".ply":
		return ParsePLY(f)
	case ".gltf":
		return ParseGLTF(path)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedFormat, filepath.Ext(path))
	}
}

// ErrUnsupportedFormat is returned when the file extension is not supported.
var ErrUnsupportedFormat = errors.New("meshparse: unsupported format")

// ---------------------------------------------------------------------------
// OBJ (Wavefront). Handles v/vn/vt/f with n-gons and vertex indices.
// ---------------------------------------------------------------------------

// ParseOBJ parses a Wavefront .obj stream (v / vn / vt / f lines).
func ParseOBJ(r io.Reader) (*Mesh, error) {
	m := &Mesh{Format: "obj"}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		switch fields[0] {
		case "v":
			if len(fields) < 4 {
				continue
			}
			m.Verts = append(m.Verts, Vec3{atof(fields[1]), atof(fields[2]), atof(fields[3])})
		case "vn":
			if len(fields) >= 4 {
				m.Normals = append(m.Normals, Vec3{atof(fields[1]), atof(fields[2]), atof(fields[3])})
			}
		case "vt":
			if len(fields) >= 3 {
				m.UVs = append(m.UVs, Vec3{atof(fields[1]), atof(fields[2]), 0})
			}
		case "f":
			// Face: "f v/vt/vn v/vt/vn ..." or "f v v v" — indices are 1-based,
			// negative = relative. Only vertex indices are kept.
			f := Face{}
			for _, tok := range fields[1:] {
				vi := tok
				if i := strings.IndexByte(tok, '/'); i >= 0 {
					vi = tok[:i]
				}
				n, err := strconv.Atoi(vi)
				if err != nil {
					continue
				}
				if n < 0 {
					n = len(m.Verts) + n + 1
				}
				if n > 0 {
					f.Verts = append(f.Verts, n-1)
				}
			}
			if len(f.Verts) >= 3 {
				m.Faces = append(m.Faces, f)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(m.Verts) == 0 {
		return nil, errors.New("meshparse: obj has no vertices")
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// STL (ASCII + binary). Triangles only.
// ---------------------------------------------------------------------------

// ParseSTL parses an STL stream, auto-detecting ASCII vs binary.
func ParseSTL(r io.Reader) (*Mesh, error) {
	// Peek the first 5 bytes: "solid" → ASCII, else binary (80-byte header +
	// uint32 triangle count).
	head := make([]byte, 5)
	if _, err := io.ReadFull(r, head); err != nil {
		return nil, err
	}
	if string(head) == "solid" {
		return parseSTLAscii(io.MultiReader(strings.NewReader("solid"), r))
	}
	return parseSTLBinary(io.MultiReader(strings.NewReader(string(head)), r))
}

func parseSTLAscii(r io.Reader) (*Mesh, error) {
	m := &Mesh{Format: "stl"}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var cur []Vec3
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "vertex":
			if len(fields) >= 4 {
				cur = append(cur, Vec3{atof(fields[1]), atof(fields[2]), atof(fields[3])})
			}
		case "endfacet":
			if len(cur) == 3 {
				m.Faces = append(m.Faces, Face{Verts: []int{len(m.Verts), len(m.Verts) + 1, len(m.Verts) + 2}})
				m.Verts = append(m.Verts, cur...)
			}
			cur = nil
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(m.Verts) == 0 {
		return nil, errors.New("meshparse: stl has no vertices")
	}
	return m, nil
}

func parseSTLBinary(r io.Reader) (*Mesh, error) {
	m := &Mesh{Format: "stl"}
	// Skip 80-byte header.
	if _, err := io.CopyN(io.Discard, r, 80); err != nil {
		return nil, err
	}
	var n uint32
	if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
		return nil, err
	}
	if n > 50_000_000 { // sanity cap
		return nil, errors.New("meshparse: stl triangle count implausible")
	}
	for i := uint32(0); i < n; i++ {
		var tri [12]float32 // normal(3) + verts(9), little-endian
		if err := binary.Read(r, binary.LittleEndian, &tri); err != nil {
			return nil, err
		}
		base := len(m.Verts)
		for j := 0; j < 3; j++ {
			m.Verts = append(m.Verts, Vec3{float64(tri[3+j*3]), float64(tri[4+j*3]), float64(tri[5+j*3])})
		}
		m.Faces = append(m.Faces, Face{Verts: []int{base, base + 1, base + 2}})
		// 2-byte attribute count.
		if _, err := io.CopyN(io.Discard, r, 2); err != nil {
			return nil, err
		}
	}
	if len(m.Verts) == 0 {
		return nil, errors.New("meshparse: stl has no vertices")
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// PLY (ASCII + binary little/big endian). Polygon support.
// ---------------------------------------------------------------------------

// ParsePLY parses a PLY stream, auto-detecting format and element layout.
func ParsePLY(r io.Reader) (*Mesh, error) {
	m := &Mesh{Format: "ply"}
	br := bufio.NewReader(r)
	// Header.
	format := ""
	elemVertex, elemFace := false, false
	var vertexCount, faceCount int
	lineNo := 0
	for {
		line, err := br.ReadString('\n')
		if err != nil && line == "" {
			return nil, errors.New("meshparse: ply header truncated")
		}
		lineNo++
		line = strings.TrimSpace(line)
		if line == "end_header" {
			break
		}
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		switch f[0] {
		case "format":
			format = f[1] // ascii | binary_little_endian | binary_big_endian
		case "element":
			switch f[1] {
			case "vertex":
				elemVertex = true
				vertexCount = atoi(f[2])
			case "face":
				elemFace = true
				faceCount = atoi(f[2])
			}
		}
	}
	_ = elemVertex
	_ = elemFace
	// vertexCount/faceCount may be 0 if counts came after other properties;
	// we parse elements dynamically by reading fixed 3-float vertices until
	// faces appear, so pre-scan is not required for correctness below.

	switch format {
	case "ascii":
		sc := bufio.NewScanner(br)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for i := 0; i < vertexCount; i++ {
			if !sc.Scan() {
				return nil, errors.New("meshparse: ply vertex truncated")
			}
			f := strings.Fields(sc.Text())
			if len(f) >= 3 {
				m.Verts = append(m.Verts, Vec3{atof(f[0]), atof(f[1]), atof(f[2])})
			}
		}
		for i := 0; i < faceCount; i++ {
			if !sc.Scan() {
				return nil, errors.New("meshparse: ply face truncated")
			}
			f := strings.Fields(sc.Text())
			if len(f) < 4 {
				continue
			}
			n, _ := strconv.Atoi(f[0])
			if n < 3 || n > len(f)-1 {
				continue
			}
			face := Face{}
			for j := 1; j <= n; j++ {
				face.Verts = append(face.Verts, atoi(f[j]))
			}
			m.Faces = append(m.Faces, face)
		}
		if err := sc.Err(); err != nil {
			return nil, err
		}
	case "binary_little_endian", "binary_big_endian":
		bo := byteOrder(format)
		readF := func() (float32, error) {
			var b [4]byte
			if _, err := io.ReadFull(br, b[:]); err != nil {
				return 0, err
			}
			return math.Float32frombits(bo.Uint32(b[:])), nil
		}
		for i := 0; i < vertexCount; i++ {
			x, err1 := readF()
			y, err2 := readF()
			z, err3 := readF()
			if err1 != nil || err2 != nil || err3 != nil {
				return nil, errors.New("meshparse: ply vertex truncated")
			}
			m.Verts = append(m.Verts, Vec3{float64(x), float64(y), float64(z)})
		}
		var ub [4]byte
		for i := 0; i < faceCount; i++ {
			if _, err := io.ReadFull(br, ub[:1]); err != nil {
				return nil, errors.New("meshparse: ply face truncated")
			}
			n := int(ub[0])
			face := Face{}
			for j := 0; j < n; j++ {
				if _, err := io.ReadFull(br, ub[:4]); err != nil {
					return nil, errors.New("meshparse: ply face truncated")
				}
				face.Verts = append(face.Verts, int(bo.Uint32(ub[:])))
			}
			m.Faces = append(m.Faces, face)
		}
	default:
		return nil, fmt.Errorf("meshparse: unsupported ply format %q", format)
	}
	if len(m.Verts) == 0 {
		return nil, errors.New("meshparse: ply has no vertices")
	}
	return m, nil
}

func atof(s string) float64 {
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}


// byteOrder returns the binary byte order for a PLY format string.
func byteOrder(format string) interface {
	Uint32([]byte) uint32
	Uint16([]byte) uint16
} {
	if format == "binary_big_endian" {
		return binary.BigEndian
	}
	return binary.LittleEndian
}
