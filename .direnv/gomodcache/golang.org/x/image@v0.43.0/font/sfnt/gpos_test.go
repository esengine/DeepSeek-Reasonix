// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sfnt_test

import (
	"encoding/binary"
	"fmt"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

func makeFontWithGPOSSubtable(subtableData []byte) ([]byte, error) {
	gposHeader := []byte{
		// GPOS Header
		0x00, 0x01, 0x00, 0x00, // Version 1.0
		0x00, 0x0a, // ScriptListOffset = 10
		0x00, 0x1e, // FeatureListOffset = 30
		0x00, 0x2c, // LookupListOffset = 44

		// Offset 10: ScriptList
		0x00, 0x01, // scriptCount = 1
		0x44, 0x46, 0x4c, 0x54, // scriptTag = 'DFLT'
		0x00, 0x08, // scriptOffset = 8 (points to Script table at 18)

		// Offset 18: Script table
		0x00, 0x04, // defaultLangSysOffset = 4 (points to LangSys table at 22)
		0x00, 0x00, // langSysCount = 0

		// Offset 22: LangSys table
		0x00, 0x00, // lookupOrder = 0
		0xff, 0xff, // requiredFeatureIndex = 0xffff
		0x00, 0x01, // featureIndexCount = 1
		0x00, 0x00, // featureIndices[0] = 0

		// Offset 30: FeatureList
		0x00, 0x01, // featureCount = 1
		0x6b, 0x65, 0x72, 0x6e, // featureTag = 'kern'
		0x00, 0x08, // featureOffset = 8 (points to Feature table at 38)

		// Offset 38: Feature table
		0x00, 0x00, // featureParamsOffset = 0
		0x00, 0x01, // lookupCount = 1
		0x00, 0x00, // lookupListIndices[0] = 0

		// Offset 44: LookupList
		0x00, 0x01, // lookupCount = 1
		0x00, 0x04, // lookupOffsets[0] = 4 (points to Lookup table at 48)

		// Offset 48: Lookup table
		0x00, 0x02, // lookupType = 2 (PairPos)
		0x00, 0x00, // lookupFlag = 0
		0x00, 0x01, // subTableCount = 1
		0x00, 0x08, // subTableOffsets[0] = 8 (points to Subtable at 56)
	}

	gposData := append(gposHeader, subtableData...)

	base := goregular.TTF
	if len(base) < 236 {
		return nil, fmt.Errorf("base font too small")
	}

	numTables := binary.BigEndian.Uint16(base[4:6])
	if numTables != 14 {
		return nil, fmt.Errorf("expected 14 tables, got %d", numTables)
	}

	// Calculate new GPOS offset.
	// It will go at the end of the file, after padding the base file to 4-byte alignment.
	gposOffset := (len(base) + 16 + 3) &^ 3
	padding := gposOffset - (len(base) + 16)

	newFont := make([]byte, gposOffset+len(gposData))
	copy(newFont[0:12], base[0:12])
	binary.BigEndian.PutUint16(newFont[4:6], 15)
	binary.BigEndian.PutUint16(newFont[6:8], 128)
	binary.BigEndian.PutUint16(newFont[8:10], 3)
	binary.BigEndian.PutUint16(newFont[10:12], 112)

	// GPOS table record at index 0:
	// tag: "GPOS" = 0x47504f53
	// checksum: 0 (ignored by sfnt package)
	// offset: GPOS offset
	// length: length of GPOS table
	binary.BigEndian.PutUint32(newFont[12:16], 0x47504f53)
	binary.BigEndian.PutUint32(newFont[16:20], 0)
	binary.BigEndian.PutUint32(newFont[20:24], uint32(gposOffset))
	binary.BigEndian.PutUint32(newFont[24:28], uint32(len(gposData)))

	// Shift original table records
	for i := range 14 {
		origRecordOffset := 12 + i*16
		newRecordOffset := 28 + i*16
		copy(newFont[newRecordOffset:newRecordOffset+16], base[origRecordOffset:origRecordOffset+16])

		// Shift offset by 16
		origOffset := binary.BigEndian.Uint32(base[origRecordOffset+8 : origRecordOffset+12])
		binary.BigEndian.PutUint32(newFont[newRecordOffset+8:newRecordOffset+12], origOffset+16)
	}

	// Copy original table data
	copy(newFont[252:], base[236:])

	// Clear padding bytes
	for i := range padding {
		newFont[gposOffset-padding+i] = 0
	}

	// Copy GPOS table data
	copy(newFont[gposOffset:], gposData)

	return newFont, nil
}

func TestGPOSFormatErrors(t *testing.T) {
	for _, test := range []struct {
		desc     string
		subtable []byte
	}{{
		desc: "PairPos format 1",
		subtable: []byte{
			0x00, 0x01, // posFormat = 1
			0x00, 0x0e, // coverageOffset = 14
			0x00, 0x04, // valueFormat1 = 0x0004
			0x00, 0x00, // valueFormat2 = 0
			0x00, 0x02, // pairSetCount = 2
			0x00, 0x16, // pairSetOffset[0] = 22
			0x00, 0x18, // pairSetOffset[1] = 24
			// Offset 14: Coverage (format 1, count 2, glyphs 0, 1)
			0x00, 0x01, // format = 1
			0x00, 0x02, // glyphCount = 2
			0x00, 0x00, // glyphArray[0] = 0
			0x00, 0x01, // glyphArray[1] = 1
			// Offset 22: PairSet 0
			0x03, 0xe8, // pairValueCount = 1000
			// Offset 24: PairSet 1 (last PairSet)
			0x00, 0x00, // pairValueCount = 0
		},
	}, {
		desc: "PairPos format 2",
		subtable: []byte{
			0x00, 0x02, // posFormat = 2
			0x00, 0x18, // coverageOffset = 24
			0x00, 0x04, // valueFormat1 = 0x0004
			0x00, 0x00, // valueFormat2 = 0
			0x00, 0x22, // classDef1Offset = 34
			0x00, 0x2a, // classDef2Offset = 42
			0x00, 0x02, // class1Count = 2
			0x00, 0x02, // class2Count = 2
			// class1Records: 2 * 2 * 2 = 8 bytes of zeroes (offset 16-24)
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			// Offset 24: Coverage (format 1, count 1, glyph 0)
			0x00, 0x01, // format = 1
			0x00, 0x01, // glyphCount = 1
			0x00, 0x00, // glyphArray[0] = 0
			// Offset 30: pad 4 bytes to reach offset 34
			0x00, 0x00, 0x00, 0x00,
			// Offset 34: ClassDef1 (format 1, startGlyph 0, count 1, class 999)
			0x00, 0x01, // format = 1
			0x00, 0x00, // startGlyph = 0
			0x00, 0x01, // glyphCount = 1
			0x03, 0xe7, // classValueArray[0] = 999
			// Offset 42: ClassDef2 (format 1, startGlyph 0, count 1, class 999)
			0x00, 0x01, // format = 1
			0x00, 0x00, // startGlyph = 0
			0x00, 0x01, // glyphCount = 1
			0x03, 0xe7, // classValueArray[0] = 999
		},
	}} {
		fontData, err := makeFontWithGPOSSubtable(test.subtable)
		if err != nil {
			t.Fatalf("failed to construct font: %v", err)
		}

		f, err := sfnt.Parse(fontData)
		if err != nil {
			t.Fatalf("failed to parse font: %v", err)
		}

		// Querying glyphs 0, 0 which maps pairValueCount = 1000 should return errInvalidGPOSTable
		_, err = f.Kern(nil, 0, 0, fixed.I(20), font.HintingNone)
		if err == nil {
			t.Fatalf("f.Kern(nil, 0, 0, fixed.I(20), font.HintingNone): succeeded, want error")
		}
	}
}
