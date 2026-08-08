package meshparse

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// ---------------------------------------------------------------------------
// glB (.glb) — the binary container form of glTF 2.0.
//
// Layout:
//
//	12-byte header: magic "glTF"(0x46546C67), version (uint32, =2), length
//	chunks:        [chunkLength uint32][chunkType uint32][data]
//	               chunkType 0x4E4F534A ("JSON") then 0x004E4942 ("BIN\0")
//
// The JSON chunk is the same glTF JSON as .gltf; buffers[0] is the BIN chunk
// (no uri).
// ---------------------------------------------------------------------------

const (
	glbMagic       = 0x46546C67 // "glTF"
	glbChunkJSON   = 0x4E4F534A // "JSON"
	glbChunkBIN    = 0x004E4942 // "BIN\0"
	glbHeaderSize  = 12
	glbChunkHeader = 8
)

// ParseGLB parses a binary .glb file into a normalized Mesh.
func ParseGLB(path string) (*Mesh, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < glbHeaderSize {
		return nil, errors.New("glb: file too short for header")
	}
	if binary.LittleEndian.Uint32(data[0:4]) != glbMagic {
		return nil, errors.New("glb: bad magic (not a glb file)")
	}
	version := binary.LittleEndian.Uint32(data[4:8])
	if version != 2 {
		return nil, fmt.Errorf("glb: unsupported version %d", version)
	}
	totalLen := int(binary.LittleEndian.Uint32(data[8:12]))
	if totalLen > len(data) {
		return nil, fmt.Errorf("glb: declared length %d exceeds file (%d)", totalLen, len(data))
	}
	data = data[:totalLen]

	var jsonBytes, binBytes []byte
	off := glbHeaderSize
	for off+glbChunkHeader <= len(data) {
		chunkLen := int(binary.LittleEndian.Uint32(data[off : off+4]))
		chunkType := binary.LittleEndian.Uint32(data[off+4 : off+8])
		start := off + glbChunkHeader
		end := start + chunkLen
		if end > len(data) {
			return nil, fmt.Errorf("glb: chunk %d exceeds file", chunkLen)
		}
		switch chunkType {
		case glbChunkJSON:
			jsonBytes = data[start:end]
		case glbChunkBIN:
			binBytes = data[start:end]
		}
		off = end
	}
	if jsonBytes == nil {
		return nil, errors.New("glb: missing JSON chunk")
	}

	var g gltfJSON
	if err := json.Unmarshal(jsonBytes, &g); err != nil {
		return nil, fmt.Errorf("glb: invalid JSON chunk: %w", err)
	}
	buffers := make([][]byte, len(g.Buffers))
	for i, b := range g.Buffers {
		if b.URI != "" {
			return nil, fmt.Errorf("glb buffer %d: uri not allowed in binary glTF", i)
		}
		if i == 0 {
			if binBytes == nil {
				return nil, errors.New("glb: missing BIN chunk for buffer 0")
			}
			if b.ByteLength < 0 {
				return nil, fmt.Errorf("glb buffer 0: negative byteLength %d", b.ByteLength)
			}
			if len(binBytes) < b.ByteLength {
				return nil, fmt.Errorf("glb buffer 0: %d bytes, need %d", len(binBytes), b.ByteLength)
			}
			buffers[0] = binBytes[:b.ByteLength]
			continue
		}
		return nil, fmt.Errorf("glb: multiple buffers unsupported (buffer %d)", i)
	}
	return parseGLTFJSON(&g, buffers, "glb")
}
