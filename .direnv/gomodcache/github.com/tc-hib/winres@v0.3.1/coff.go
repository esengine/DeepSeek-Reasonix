package winres

import (
	"debug/pe"
	"encoding/binary"
	"errors"
	"io"
)

// https://docs.microsoft.com/en-us/windows/win32/debug/pe-format#other-contents-of-the-file

const (
	_IMAGE_SCN_MEM_READ             = 0x00000040
	_IMAGE_SCN_CNT_INITIALIZED_DATA = 0x40000000
)

const sizeOfReloc = 10

func writeObject(w io.Writer, r *ResourceSet, arch Arch) error {
	file := pe.FileHeader{
		Machine:          pe.IMAGE_FILE_MACHINE_UNKNOWN,
		NumberOfSections: 1,
		NumberOfSymbols:  1,
	}
	section := pe.SectionHeader32{
		Name:                 [8]byte{'.', 'r', 's', 'r', 'c'},
		PointerToLineNumbers: 0,
		Characteristics:      _IMAGE_SCN_MEM_READ | _IMAGE_SCN_CNT_INITIALIZED_DATA,
	}

	section.PointerToRawData = uint32(binary.Size(file) + binary.Size(section))
	section.SizeOfRawData = uint32(r.fullSize())
	section.PointerToRelocations = section.PointerToRawData + section.SizeOfRawData
	section.NumberOfRelocations = uint16(r.numDataEntries())

	switch arch {
	case ArchI386:
		file.Machine = pe.IMAGE_FILE_MACHINE_I386
	case ArchAMD64:
		file.Machine = pe.IMAGE_FILE_MACHINE_AMD64
	case ArchARM:
		file.Machine = pe.IMAGE_FILE_MACHINE_ARMNT
	case ArchARM64:
		file.Machine = pe.IMAGE_FILE_MACHINE_ARM64
	default:
		return errors.New(errUnknownArch)
	}
	file.PointerToSymbolTable = section.PointerToRelocations + uint32(section.NumberOfRelocations)*sizeOfReloc

	if err := binary.Write(w, binary.LittleEndian, file); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, section); err != nil {
		return err
	}
	addr, err := r.write(w)
	if err != nil {
		return err
	}
	if err := writeRelocTable(w, 0, arch, addr); err != nil {
		return err
	}
	if err := writeSymbol(w, 1); err != nil {
		return err
	}
	// Empty string table
	if err := binary.Write(w, binary.LittleEndian, uint32(4)); err != nil {
		return err
	}
	return nil
}

// https://docs.microsoft.com/en-us/windows/win32/debug/pe-format#type-indicators

const (
	_IMAGE_REL_I386_DIR32NB   uint16 = 0x7
	_IMAGE_REL_AMD64_ADDR32NB uint16 = 0x3
	_IMAGE_REL_ARM_ADDR32NB   uint16 = 0x2
	_IMAGE_REL_ARM64_ADDR32NB uint16 = 0x2
)

func writeRelocTable(w io.Writer, symbolIndex int, arch Arch, addr []int) error {
	var t uint16

	switch arch {
	case ArchI386:
		t = _IMAGE_REL_I386_DIR32NB
	case ArchAMD64:
		t = _IMAGE_REL_AMD64_ADDR32NB
	case ArchARM:
		t = _IMAGE_REL_ARM_ADDR32NB
	case ArchARM64:
		t = _IMAGE_REL_ARM64_ADDR32NB
	default:
		return errors.New(errUnknownArch)
	}

	for _, a := range addr {
		err := binary.Write(w, binary.LittleEndian, &pe.Reloc{
			VirtualAddress:   uint32(a),
			SymbolTableIndex: uint32(symbolIndex),
			Type:             t,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// https://docs.microsoft.com/en-us/windows/win32/debug/pe-format#coff-symbol-table

const _IMAGE_SYM_TYPE_NULL = 0

const _IMAGE_SYM_CLASS_STATIC = 3

func writeSymbol(w io.Writer, sectionNumber int) error {
	// The symbol is a section name because Value == 0 and StorageClass == IMAGE_SYM_CLASS_STATIC
	return binary.Write(w, binary.LittleEndian, &pe.COFFSymbol{
		Name:          [8]byte{'.', 'r', 's', 'r', 'c'},
		Value:         0,
		SectionNumber: int16(sectionNumber),
		Type:          _IMAGE_SYM_TYPE_NULL,
		StorageClass:  _IMAGE_SYM_CLASS_STATIC,
	})
}
