package normalize

import (
	"encoding/binary"
	"errors"
	"io"
)

// ParseMP4Duration parses MP4 video duration (ms) by walking the ISO BMFF box
// hierarchy and finding `moov -> mvhd`.
func ParseMP4Duration(r io.ReadSeeker) (int, error) {
	// Start from beginning
	_, err := r.Seek(0, io.SeekStart)
	if err != nil {
		return 0, err
	}

	size, err := getFileSize(r)
	if err != nil {
		return 0, err
	}

	moovStart, moovEnd, err := findBoxPayload(r, 0, size, "moov")
	if err != nil {
		return 0, err
	}

	mvhdStart, _, err := findBoxPayload(r, moovStart, moovEnd, "mvhd")
	if err != nil {
		return 0, err
	}

	_, err = r.Seek(mvhdStart, io.SeekStart)
	if err != nil {
		return 0, err
	}

	header := make([]byte, 4)
	if _, err := io.ReadFull(r, header); err != nil {
		return 0, err
	}

	version := header[0]
	// skip flags (3 bytes)

	var timescale uint32
	var duration float64

	if version == 1 {
		// creation(8) + modification(8) + timescale(4) + duration(8)
		data := make([]byte, 28)
		if _, err := io.ReadFull(r, data); err != nil {
			return 0, err
		}
		timescale = binary.BigEndian.Uint32(data[16:20])
		duration = float64(binary.BigEndian.Uint64(data[20:28]))
	} else {
		// creation(4) + modification(4) + timescale(4) + duration(4)
		data := make([]byte, 16)
		if _, err := io.ReadFull(r, data); err != nil {
			return 0, err
		}
		timescale = binary.BigEndian.Uint32(data[8:12])
		duration = float64(binary.BigEndian.Uint32(data[12:16]))
	}

	if timescale == 0 {
		return 0, errors.New("invalid timescale")
	}

	return int((duration / float64(timescale)) * 1000), nil
}

func getFileSize(r io.ReadSeeker) (int64, error) {
	current, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}
	size, err := r.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}
	_, err = r.Seek(current, io.SeekStart)
	if err != nil {
		return 0, err
	}
	return size, nil
}

func findBoxPayload(r io.ReadSeeker, begin int64, end int64, name string) (int64, int64, error) {
	p := begin
	header := make([]byte, 8)

	for p+8 <= end {
		_, err := r.Seek(p, io.SeekStart)
		if err != nil {
			return 0, 0, err
		}

		if _, err := io.ReadFull(r, header); err != nil {
			return 0, 0, err
		}

		size := uint64(binary.BigEndian.Uint32(header[0:4]))
		boxType := string(header[4:8])

		var boxEnd int64
		var payloadStart int64

		if size == 1 {
			// 64-bit large size
			largeSizeBuf := make([]byte, 8)
			if _, err := io.ReadFull(r, largeSizeBuf); err != nil {
				return 0, 0, err
			}
			largeSize := binary.BigEndian.Uint64(largeSizeBuf)
			boxEnd = p + int64(largeSize)
			payloadStart = p + 16
		} else if size == 0 {
			boxEnd = end
			payloadStart = p + 8
		} else {
			boxEnd = p + int64(size)
			payloadStart = p + 8
		}

		if boxEnd <= p || boxEnd > end {
			return 0, 0, errors.New("invalid box size")
		}

		if boxType == name {
			return payloadStart, boxEnd, nil
		}

		p = boxEnd
	}

	return 0, 0, errors.New("box not found")
}
