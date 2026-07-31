package winres

import (
	"encoding/binary"
	"io"
)

// readFull is like io.ReadFull, except it always returns io.ErrUnexpectedEOF instead of io.EOF.
func readFull(r io.Reader, data []byte) error {
	_, err := io.ReadFull(r, data)
	if err == io.EOF {
		return io.ErrUnexpectedEOF
	}
	return err
}

// binaryRead is like binary.Read, except it always returns io.ErrUnexpectedEOF instead of io.EOF.
// Furthermore, it always uses binary.LittleEndian.
func binaryRead(r io.Reader, v interface{}) error {
	err := binary.Read(r, binary.LittleEndian, v)
	if err == io.EOF {
		return io.ErrUnexpectedEOF
	}
	return err
}
