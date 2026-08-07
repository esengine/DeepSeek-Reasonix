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
	"strings"
)

// ---------------------------------------------------------------------------
// Voxel support (docs/MODELLING_OVERHAUL.md §4). Voxels are first-class:
// .vox (MagicaVoxel) parsing + mesh→voxel voxelization. Pure Go, no deps.
// ---------------------------------------------------------------------------

// VoxelModel is a normalized voxel model: dense 3D occupancy grid + palette.
type VoxelModel struct {
	// Size of the grid (W×H×D).
	Size   [3]int  `json:"size"`
	Voxels []Voxel `json:"voxels,omitempty"`
	// Palette colors (RGBA), palette[0] = index 1 in .vox.
	Palette [][4]byte `json:"palette,omitempty"`
}

// Voxel is one occupied cell with a palette color index (1-based).
type Voxel struct {
	X, Y, Z int
	Color   uint8 // 1..254 palette index (0 = empty)
}

// VoxelDescriptor is the compact voxel summary (~20 token).
type VoxelDescriptor struct {
	Format    string  `json:"format"`
	Size      [3]int  `json:"size"`
	Filled    int     `json:"filled"`
	Colors    int     `json:"colors"`
	Comps     int     `json:"components"`
	Solidity  float64 `json:"solidity"` // filled / (w*h*d)
	HasHoles  bool    `json:"has_holes"`
}

// ParseVox parses a MagicaVoxel .vox file (chunk-based, palette + XYZI).
func ParseVox(path string) (*VoxelModel, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Header: "VOX " + version.
	var magic [4]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return nil, err
	}
	if string(magic[:]) != "VOX " {
		return nil, errors.New("meshparse: not a .vox file")
	}
	var version uint32
	if err := binary.Read(f, binary.LittleEndian, &version); err != nil {
		return nil, err
	}
	_ = version

	vm := &VoxelModel{}
	// Read chunks until EOF (main → SIZE/XYZI/PALETTE/RGBA…).
	for {
		chunkID, content, children, err := readVoxChunk(f)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		switch chunkID {
		case "SIZE":
			if len(content) >= 12 {
				vm.Size = [3]int{
					int(binary.LittleEndian.Uint32(content[0:4])),
					int(binary.LittleEndian.Uint32(content[4:8])),
					int(binary.LittleEndian.Uint32(content[8:12])),
				}
			}
		case "XYZI":
			if len(content) >= 4 {
				n := int(binary.LittleEndian.Uint32(content[0:4]))
				buf := content[4:]
				for i := 0; i < n && i*4+4 <= len(buf); i++ {
					vm.Voxels = append(vm.Voxels, Voxel{
						X:     int(buf[i*4]),
						Y:     int(buf[i*4+1]),
						Z:     int(buf[i*4+2]),
						Color: uint8(buf[i*4+3]),
					})
				}
			}
		case "RGBA": // palette, 256 entries, 4 bytes each; index 0 unused.
			if len(content) >= 256*4 {
				pal := make([][4]byte, 256)
				for i := 0; i < 256; i++ {
					copy(pal[i][:], content[i*4:i*4+4])
				}
				vm.Palette = pal
			}
		}
		// Skip children (we don't need nested chunks).
		_ = children
	}
	if vm.Size == [3]int{} {
		return nil, errors.New("meshparse: .vox missing SIZE chunk")
	}
	return vm, nil
}

// readVoxChunk reads one chunk: id(4) + contentSize(4) + childrenSize(4).
func readVoxChunk(r io.Reader) (id string, content []byte, children []byte, err error) {
	var hdr [12]byte
	if _, err = io.ReadFull(r, hdr[:]); err != nil {
		return "", nil, nil, err
	}
	id = string(hdr[0:4])
	cs := binary.LittleEndian.Uint32(hdr[4:8])
	kids := binary.LittleEndian.Uint32(hdr[8:12])
	content = make([]byte, cs)
	if _, err = io.ReadFull(r, content); err != nil {
		return "", nil, nil, err
	}
	children = make([]byte, kids)
	if _, err = io.ReadFull(r, children); err != nil {
		return "", nil, nil, err
	}
	return id, content, children, nil
}

// AnalyzeVox computes the compact voxel descriptor.
func AnalyzeVox(vm *VoxelModel) VoxelDescriptor {
	d := VoxelDescriptor{
		Format:   "vox",
		Size:     vm.Size,
		Filled:   len(vm.Voxels),
		HasHoles: false,
	}
	// Colors + solidity.
	colorSet := map[uint8]struct{}{}
	w, h, dd := vm.Size[0], vm.Size[1], vm.Size[2]
	for _, v := range vm.Voxels {
		if v.Color != 0 {
			colorSet[v.Color] = struct{}{}
		}
	}
	d.Colors = len(colorSet)
	if w > 0 && h > 0 && dd > 0 {
		d.Solidity = math.Round(float64(len(vm.Voxels))/float64(w*h*dd)*1000) / 1000
	}
	// Components via 6-connected flood fill on the occupied set.
	occ := map[[3]int]struct{}{}
	for _, v := range vm.Voxels {
		occ[[3]int{v.X, v.Y, v.Z}] = struct{}{}
	}
	seen := map[[3]int]struct{}{}
	dirs := [6][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}}
	var flood func(p [3]int)
	flood = func(p [3]int) {
		seen[p] = struct{}{}
		for _, d := range dirs {
			q := [3]int{p[0] + d[0], p[1] + d[1], p[2] + d[2]}
			if _, ok := occ[q]; ok {
				if _, s := seen[q]; !s {
					flood(q)
				}
			}
		}
	}
	for p := range occ {
		if _, s := seen[p]; !s {
			d.Comps++
			flood(p)
		}
	}
	// Hole heuristic: interior empty cells adjacent to ≥3 occupied neighbors.
	if len(vm.Voxels) > 0 && d.Comps > 0 && d.Solidity < 0.95 {
		// Conservative: treat large interior gaps as possible holes.
		d.HasHoles = d.Solidity < 0.5
	}
	return d
}

// Voxelize converts a closed mesh into a voxel grid of the given resolution
// (longest axis). Occupancy = point-in-mesh via even-odd ray casting at cell
// centers (fast, O(cells × faces) but capped). Returns the voxel model.
func Voxelize(m *Mesh, resolution int) (*VoxelModel, error) {
	if len(m.Verts) == 0 || len(m.Faces) == 0 {
		return nil, errors.New("meshparse: voxelize requires a non-empty mesh")
	}
	if resolution < 4 || resolution > 512 {
		return nil, fmt.Errorf("meshparse: resolution %d out of range [4,512]", resolution)
	}
	minV, maxV := m.Bounds()
	size := Vec3{maxV.X - minV.X, maxV.Y - minV.Y, maxV.Z - minV.Z}
	// Scale so longest axis = resolution.
	longest := math.Max(size.X, math.Max(size.Y, size.Z))
	if longest < 1e-12 {
		return nil, errors.New("meshparse: degenerate mesh bounds")
	}
	scale := float64(resolution) / longest
	w := max(1, int(math.Round(size.X*scale)))
	h := max(1, int(math.Round(size.Y*scale)))
	d := max(1, int(math.Round(size.Z*scale)))

	// Even-odd ray casting: for each cell center, cast ray +X, count crossings.
	// Robust variant: use the NEAREST triangle intersection (t>0); the cell is
	// inside iff that nearest hit is an exit face (face normal · ray < 0).
	// Shared edges (diagonals) are hit by two triangles at ≈same t with the
	// same exit/enter classification, so double-counting cannot flip parity.
	tris := triangulate(m)
	vm := &VoxelModel{Size: [3]int{w, h, d}}
	inside := make([]bool, w*h*d)
	for x := 0; x < w; x++ {
		cx := minV.X + (float64(x)+0.5)/scale
		for y := 0; y < h; y++ {
			cy := minV.Y + (float64(y)+0.5)/scale
			for z := 0; z < d; z++ {
				cz := minV.Z + (float64(z)+0.5)/scale
				best := math.Inf(1)
				bestExit := false
				for _, t := range tris {
					tHit, exit, ok := rayTriangleNearest(cx, cy, cz, t)
					if !ok || tHit >= best {
						continue
					}
					best = tHit
					bestExit = exit
				}
				inside[x+w*(y+h*z)] = bestExit
			}
		}
	}
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			for z := 0; z < d; z++ {
				if inside[x+w*(y+h*z)] {
					vm.Voxels = append(vm.Voxels, Voxel{X: x, Y: y, Z: z, Color: 1})
				}
			}
		}
	}
	// Palette: single color white.
	vm.Palette = make([][4]byte, 256)
	vm.Palette[1] = [4]byte{255, 255, 255, 255}
	return vm, nil
}

// triangulate splits n-gons into triangles (fan).
func triangulate(m *Mesh) [][3]Vec3 {
	var out [][3]Vec3
	for _, f := range m.Faces {
		if len(f.Verts) < 3 {
			continue
		}
		p0 := m.Verts[f.Verts[0]]
		for i := 1; i+1 < len(f.Verts); i++ {
			out = append(out, [3]Vec3{p0, m.Verts[f.Verts[i]], m.Verts[f.Verts[i+1]]})
		}
	}
	return out
}

// rayTriangleNearest returns the ray parameter t of the +X ray from p hitting
// the triangle (t>0), whether that hit is an exit face (face normal · ray < 0),
// and ok=false if the ray misses or hits edge-on.
func rayTriangleNearest(px, py, pz float64, tri [3]Vec3) (tHit float64, exit, ok bool) {
	a, b, c := tri[0], tri[1], tri[2]
	e1 := Vec3{b.X - a.X, b.Y - a.Y, b.Z - a.Z}
	e2 := Vec3{c.X - a.X, c.Y - a.Y, c.Z - a.Z}
	// Normal.
	nx := e1.Y*e2.Z - e1.Z*e2.Y
	ny := e1.Z*e2.X - e1.X*e2.Z
	nz := e1.X*e2.Y - e1.Y*e2.X
	denom := nx // ray dir = (1,0,0)
	if math.Abs(denom) < 1e-12 {
		return 0, false, false // edge-on: skip
	}
	t := (nx*(a.X-px) + ny*(a.Y-py) + nz*(a.Z-pz)) / denom
	if t <= 1e-9 {
		return 0, false, false
	}
	ix, iy, iz := px+t, py, pz
	// Barycentric (a as origin, b and c as basis).
	d00 := dot(e2, e2)
	d01 := dot(e2, e1)
	d11 := dot(e1, e1)
	v2 := Vec3{ix - a.X, iy - a.Y, iz - a.Z}
	d20 := dot(v2, e2)
	d21 := dot(v2, e1)
	den := d00*d11 - d01*d01
	if math.Abs(den) < 1e-12 {
		return 0, false, false
	}
	u := (d11*d20 - d01*d21) / den
	v := (d00*d21 - d01*d20) / den
	if u < -1e-9 || v < -1e-9 || u+v > 1+1e-9 {
		return 0, false, false
	}
	return t, denom < 0, true
}

func dot(a, b Vec3) float64 { return a.X*b.X + a.Y*b.Y + a.Z*b.Z }

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// WriteVox writes a MagicaVoxel .vox file (SIZE + XYZI + RGBA).
func (vm *VoxelModel) WriteVox(path string) error {
	if len(vm.Voxels) > 0xFFFFFF {
		return errors.New("meshparse: too many voxels to write")
	}
	var b strings.Builder
	buf := bufio.NewWriter(&b)
	writeU32(buf, 0x20584F56) // "VOX "
	writeU32(buf, 150)        // version
	writeVoxChunk(buf, "MAIN", nil)
	writeVoxChunk(buf, "SIZE", []byte{
		byte(vm.Size[0]), byte(vm.Size[0] >> 8), byte(vm.Size[0] >> 16), byte(vm.Size[0] >> 24),
		byte(vm.Size[1]), byte(vm.Size[1] >> 8), byte(vm.Size[1] >> 16), byte(vm.Size[1] >> 24),
		byte(vm.Size[2]), byte(vm.Size[2] >> 8), byte(vm.Size[2] >> 16), byte(vm.Size[2] >> 24),
	})
	xyzi := make([]byte, 4+len(vm.Voxels)*4)
	putU32(xyzi[0:4], uint32(len(vm.Voxels)))
	for i, v := range vm.Voxels {
		xyzi[4+i*4] = byte(v.X)
		xyzi[4+i*4+1] = byte(v.Y)
		xyzi[4+i*4+2] = byte(v.Z)
		xyzi[4+i*4+3] = v.Color
	}
	writeVoxChunk(buf, "XYZI", xyzi)
	if vm.Palette != nil {
		rgba := make([]byte, 256*4)
		for i := 0; i < 256 && i < len(vm.Palette); i++ {
			copy(rgba[i*4:i*4+4], vm.Palette[i][:])
		}
		writeVoxChunk(buf, "RGBA", rgba)
	}
	if err := buf.Flush(); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func writeU32(w *bufio.Writer, v uint32) {
	var b [4]byte
	putU32(b[:], v)
	w.Write(b[:])
}

func putU32(dst []byte, v uint32) {
	dst[0] = byte(v)
	dst[1] = byte(v >> 8)
	dst[2] = byte(v >> 16)
	dst[3] = byte(v >> 24)
}

func writeVoxChunk(w *bufio.Writer, id string, content []byte) {
	w.WriteString(id)
	writeU32(w, uint32(len(content)))
	writeU32(w, 0)
	w.Write(content)
}

// ParseVoxPath is a convenience: ParseVox by extension guard.
func ParseVoxPath(path string) (*VoxelModel, error) {
	if strings.ToLower(filepath.Ext(path)) != ".vox" {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedFormat, path)
	}
	return ParseVox(path)
}
