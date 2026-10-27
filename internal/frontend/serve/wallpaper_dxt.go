package serve

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/png"
)

// dxtFromTEX decodes the first mip of a DXT-compressed TEX. Wallpaper Engine
// writes TEX headers as 9-byte strings, so every following int32 is unaligned.
func dxtFromTEX(raw []byte) ([]byte, string, bool) {
	if !dxtMagic(raw) {
		return nil, "", false
	}
	format := int(int32(binary.LittleEndian.Uint32(raw[18:22])))
	blockSize := dxtBlockSize(format)
	if blockSize == 0 {
		return nil, "", false
	}
	texW := int(int32(binary.LittleEndian.Uint32(raw[26:30])))
	texH := int(int32(binary.LittleEndian.Uint32(raw[30:34])))
	imgW := int(int32(binary.LittleEndian.Uint32(raw[34:38])))
	imgH := int(int32(binary.LittleEndian.Uint32(raw[38:42])))
	if texW <= 0 || texH <= 0 || imgW <= 0 || imgH <= 0 || texW > 32768 || texH > 32768 {
		return nil, "", false
	}
	if !dxtImageFits(texW, texH, imgW, imgH) {
		return nil, "", false
	}
	if int64(texW)*int64(texH) > maxSceneImagePixels {
		return nil, "", false
	}
	block := bytes.Index(raw[42:], []byte("TEXB"))
	if block < 0 || block+9 > len(raw) {
		return nil, "", false
	}
	block += 42
	start := block + 9
	// imageCount is followed by 0..2 version-dependent fields, then mipmapCount.
	for skip := range 3 {
		p := start + 4 + skip*4
		if p+4 > len(raw) {
			continue
		}
		mips := int(int32(binary.LittleEndian.Uint32(raw[p : p+4])))
		p += 4
		if mips < 1 || mips > 16 {
			continue
		}
		for i := range mips {
			if p+20 > len(raw) {
				break
			}
			w := int(int32(binary.LittleEndian.Uint32(raw[p : p+4])))
			h := int(int32(binary.LittleEndian.Uint32(raw[p+4 : p+8])))
			compression := int(int32(binary.LittleEndian.Uint32(raw[p+8 : p+12])))
			rawSize := int(binary.LittleEndian.Uint32(raw[p+12 : p+16]))
			stored := int(binary.LittleEndian.Uint32(raw[p+16 : p+20]))
			p += 20
			if stored < 0 || p+stored > len(raw) {
				break
			}
			if i == 0 && w == texW && h == texH {
				data := raw[p : p+stored]
				if compression == 1 {
					var err error
					data, err = lz4Block(data, rawSize)
					if err != nil {
						return nil, "", false
					}
				}
				out, err := decodeDXT(data, texW, texH, blockSize, format)
				if err != nil {
					return nil, "", false
				}
				if imgW < texW || imgH < texH {
					out = cropRGBA(out, texW, imgW, imgH)
				}
				var buf bytes.Buffer
				if err := png.Encode(&buf, &image.NRGBA{Pix: out, Stride: imgW * 4, Rect: image.Rect(0, 0, imgW, imgH)}); err != nil {
					return nil, "", false
				}
				return buf.Bytes(), "image/png", true
			}
			p += stored
		}
	}
	return nil, "", false
}

func dxtMagic(raw []byte) bool {
	return len(raw) >= 46 && bytes.HasPrefix(raw, []byte("TEXV0005\x00TEXI0001\x00"))
}

func dxtImageFits(texW, texH, imgW, imgH int) bool {
	return imgW <= texW && imgH <= texH
}

func dxtBlockSize(format int) int {
	switch format {
	case 1, 2, 4:
		return 16
	case 7:
		return 8
	default:
		return 0
	}
}

func cropRGBA(pix []byte, stride, width, height int) []byte {
	out := make([]byte, width*height*4)
	for y := range height {
		copy(out[y*width*4:(y+1)*width*4], pix[y*stride*4:y*stride*4+width*4])
	}
	return out
}

func decodeDXT(data []byte, width, height, blockSize, format int) ([]byte, error) {
	blocksX := (width + 3) / 4
	blocksY := (height + 3) / 4
	need := blocksX * blocksY * blockSize
	if len(data) < need {
		return nil, errors.New("DXT data is short")
	}
	out := make([]byte, width*height*4)
	for by := range blocksY {
		for bx := range blocksX {
			block := data[(by*blocksX+bx)*blockSize:]
			color := block
			alpha := []byte(nil)
			if format != 7 {
				alpha, color = block[:8], block[8:16]
			}
			c0 := binary.LittleEndian.Uint16(color[0:2])
			c1 := binary.LittleEndian.Uint16(color[2:4])
			palette := dxtPalette(c0, c1, format == 7 && c0 <= c1)
			bits := binary.LittleEndian.Uint32(color[4:8])
			for py := range 4 {
				for px := range 4 {
					x, y := bx*4+px, by*4+py
					if x >= width || y >= height {
						continue
					}
					idx := int((bits >> (2 * (py*4 + px))) & 3)
					r, g, b, a := palette[idx][0], palette[idx][1], palette[idx][2], uint8(255)
					if len(palette[idx]) > 3 {
						a = palette[idx][3]
					}
					if alpha != nil {
						switch format {
						case 1, 2:
							a = alpha[(py*4+px)/2]
							if (py*4+px)%2 == 0 {
								a = (a & 0x0f) * 17
							} else {
								a = (a >> 4) * 17
							}
						case 4:
							a = dxt5Alpha(alpha, py*4+px)
						}
					}
					at := (y*width + x) * 4
					out[at], out[at+1], out[at+2], out[at+3] = r, g, b, a
				}
			}
		}
	}
	return out, nil
}

func dxtPalette(c0, c1 uint16, three bool) [][4]uint8 {
	r0, g0, b0 := rgb565(c0)
	r1, g1, b1 := rgb565(c1)
	p := [][4]uint8{{r0, g0, b0, 255}, {r1, g1, b1, 255}}
	if three {
		p = append(p, [4]uint8{uint8((int(r0) + int(r1)) / 2), uint8((int(g0) + int(g1)) / 2), uint8((int(b0) + int(b1)) / 2), 255}, [4]uint8{0, 0, 0, 0})
	} else {
		p = append(p,
			[4]uint8{uint8((2*int(r0) + int(r1)) / 3), uint8((2*int(g0) + int(g1)) / 3), uint8((2*int(b0) + int(b1)) / 3), 255},
			[4]uint8{uint8((int(r0) + 2*int(r1)) / 3), uint8((int(g0) + 2*int(g1)) / 3), uint8((int(b0) + 2*int(b1)) / 3), 255})
	}
	return p
}

func rgb565(value uint16) (uint8, uint8, uint8) {
	r := uint8((value >> 11) & 0x1f)
	g := uint8((value >> 5) & 0x3f)
	b := uint8(value & 0x1f)
	return r<<3 | r>>2, g<<2 | g>>4, b<<3 | b>>2
}

func dxt5Alpha(alpha []byte, pixel int) uint8 {
	a0, a1 := alpha[0], alpha[1]
	table := [8]uint8{a0, a1, 0, 0, 0, 0, 0, 0}
	if a0 > a1 {
		for i := 2; i < 8; i++ {
			table[i] = uint8(((8-i)*int(a0) + (i-1)*int(a1)) / 7)
		}
	} else {
		for i := 2; i < 6; i++ {
			table[i] = uint8(((6-i)*int(a0) + (i-1)*int(a1)) / 5)
		}
		table[6], table[7] = 0, 255
	}
	bits := uint64(0)
	for i := range 6 {
		bits |= uint64(alpha[2+i]) << (8 * i)
	}
	return table[(bits>>uint(3*pixel))&7]
}

func lz4Block(src []byte, size int) ([]byte, error) {
	if size < 0 || size > 256<<20 {
		return nil, errors.New("invalid LZ4 size")
	}
	out := make([]byte, 0, min(size, len(src)*255))
	for i := 0; i < len(src); {
		token := src[i]
		i++
		literal := int(token >> 4)
		if literal == 15 {
			for i < len(src) {
				n := int(src[i])
				i++
				literal += n
				if n != 255 {
					break
				}
			}
		}
		if i+literal > len(src) || len(out)+literal > size {
			return nil, errors.New("invalid LZ4 literal")
		}
		out = append(out, src[i:i+literal]...)
		i += literal
		if i == len(src) {
			break
		}
		if i+2 > len(src) {
			return nil, errors.New("invalid LZ4 offset")
		}
		offset := int(src[i]) | int(src[i+1])<<8
		i += 2
		if offset == 0 || offset > len(out) {
			return nil, errors.New("invalid LZ4 offset")
		}
		match := int(token & 0x0f)
		if match == 15 {
			for i < len(src) {
				n := int(src[i])
				i++
				match += n
				if n != 255 {
					break
				}
			}
		}
		match += 4
		if len(out)+match > size {
			return nil, errors.New("invalid LZ4 match")
		}
		for range match {
			out = append(out, out[len(out)-offset])
		}
	}
	if len(out) != size {
		return nil, errors.New("LZ4 output is short")
	}
	return out, nil
}
