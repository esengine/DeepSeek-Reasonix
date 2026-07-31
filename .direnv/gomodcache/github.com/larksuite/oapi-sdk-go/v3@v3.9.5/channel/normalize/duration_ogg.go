package normalize

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
)

// ParseOpusDuration parses Opus/OGG audio duration (ms) from an io.ReadSeeker.
// Scans backward for the last "OggS" page capture pattern.
func ParseOpusDuration(r io.ReadSeeker) (int, error) {
	size, err := getFileSize(r)
	if err != nil {
		return 0, err
	}

	if size < 27 {
		return 0, errors.New("file too small to be ogg")
	}

	// Read up to last 65536 bytes
	readSize := int64(65536)
	if size < readSize {
		readSize = size
	}

	buf := make([]byte, readSize)
	_, err = r.Seek(-readSize, io.SeekEnd)
	if err != nil {
		return 0, err
	}

	if _, err := io.ReadFull(r, buf); err != nil {
		return 0, err
	}

	for i := len(buf) - 27; i >= 0; i-- {
		if buf[i] == 0x4f && buf[i+1] == 0x67 && buf[i+2] == 0x67 && buf[i+3] == 0x53 {
			// Found "OggS"
			granule := int64(binary.LittleEndian.Uint64(buf[i+6 : i+14]))
			if granule < 0 {
				return 0, errors.New("invalid granule position")
			}
			ms := float64(granule) / 48.0
			if ms < 0 || math.IsNaN(ms) || math.IsInf(ms, 0) {
				return 0, errors.New("invalid duration")
			}
			return int(math.Round(ms)), nil
		}
	}

	return 0, errors.New("OggS not found")
}
