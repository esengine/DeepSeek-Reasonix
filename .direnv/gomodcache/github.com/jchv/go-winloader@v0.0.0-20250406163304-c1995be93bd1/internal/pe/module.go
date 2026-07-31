package pe

import (
	"encoding/binary"
	"errors"
	"io"
)

var (
	// ErrBadMZSignature is returned when the MZ signature is invalid.
	ErrBadMZSignature = errors.New("mz: bad signature")

	// ErrBadPESignature is returned when the PE signature is invalid.
	ErrBadPESignature = errors.New("pe: bad signature")

	// ErrUnknownOptionalHeaderMagic is returned when the optional header
	// magic field has an unknown value.
	ErrUnknownOptionalHeaderMagic = errors.New("pe: unknown optional header magic")
)

// Module contains a parsed and loaded PE file.
type Module struct {
	IsPE64    bool
	DOSHeader ImageDOSHeader
	Header    ImageNTHeaders64
	Sections  []ImageSectionHeader
}

// LoadModule loads a PE module into memory.
func LoadModule(r io.ReadSeeker) (*Module, error) {
	m := &Module{}

	r.Seek(0, io.SeekStart)
	dos := ImageDOSHeader{}
	if err := binary.Read(r, binary.LittleEndian, &dos); err != nil {
		return nil, err
	}
	if dos.Signature != MZSignature {
		return nil, ErrBadMZSignature
	}
	m.DOSHeader = dos

	r.Seek(int64(dos.NewHeaderAddr), io.SeekStart)
	pesig := [4]byte{}
	if err := binary.Read(r, binary.LittleEndian, &pesig); err != nil {
		return nil, err
	}

	if pesig != PESignature {
		return nil, ErrBadPESignature
	}

	optmagic := uint16(0)
	r.Seek(int64(dos.NewHeaderAddr)+OffsetOfOptionalHeaderFromNTHeader, io.SeekStart)
	if err := binary.Read(r, binary.LittleEndian, &optmagic); err != nil {
		return nil, err
	}

	r.Seek(int64(dos.NewHeaderAddr), io.SeekStart)
	switch optmagic {
	case ImageNTOptionalHeader32Magic:
		m.IsPE64 = false
		nt := ImageNTHeaders32{}
		if err := binary.Read(r, binary.LittleEndian, &nt); err != nil {
			return nil, err
		}
		m.Header = nt.To64()
	case ImageNTOptionalHeader64Magic:
		m.IsPE64 = true
		nt := ImageNTHeaders64{}
		if err := binary.Read(r, binary.LittleEndian, &nt); err != nil {
			return nil, err
		}
		m.Header = nt
	default:
		return nil, ErrUnknownOptionalHeaderMagic
	}

	// Seek past end of optional headers.
	r.Seek(int64(dos.NewHeaderAddr)+OffsetOfOptionalHeaderFromNTHeader+int64(m.Header.FileHeader.SizeOfOptionalHeader), io.SeekStart)

	for i := uint16(0); i < m.Header.FileHeader.NumberOfSections; i++ {
		section := ImageSectionHeader{}
		if err := binary.Read(r, binary.LittleEndian, &section); err != nil {
			return nil, err
		}
		m.Sections = append(m.Sections, section)
	}

	return m, nil
}
