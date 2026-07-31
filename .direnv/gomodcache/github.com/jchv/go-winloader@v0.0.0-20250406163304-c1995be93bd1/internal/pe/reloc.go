package pe

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// BaseRelocation is a parsed PE base relocation.
type BaseRelocation struct {
	// Relative virtual address of offset (that is, 0 == first byte of PE.)
	Offset uint64

	// Type of base relocation using the ImageRelBased* constants.
	Type int
}

// LoadBaseRelocs loads relocations from memory.
func LoadBaseRelocs(m *Module, mem io.ReadSeeker) []BaseRelocation {
	relocs := []BaseRelocation{}
	dir := m.Header.OptionalHeader.DataDirectory[ImageDirectoryEntryBaseReloc]
	if dir.Size == 0 {
		return relocs
	}
	n := uint32(0)
	mem.Seek(int64(dir.VirtualAddress), io.SeekStart)
	for n < dir.Size {
		hdr := ImageBaseRelocation{}
		binary.Read(mem, binary.LittleEndian, &hdr)
		data := make([]uint16, hdr.SizeOfBlock/2-4)
		binary.Read(mem, binary.LittleEndian, &data)
		for _, i := range data {
			relocs = append(relocs, BaseRelocation{
				Offset: uint64(hdr.VirtualAddress) + uint64(i&0xFFF),
				Type:   int(i >> 12),
			})
		}
		n += hdr.SizeOfBlock
	}
	return relocs
}

// Relocate performs a series of relocations on m, where address is the load
// address and original is the original address. The ReadWriteSeeker is
// assumed to have the PE image at offset 0.
func Relocate(machine int, rels []BaseRelocation, address uint64, original uint64, m io.ReadWriteSeeker, o binary.ByteOrder) error {
	b := [8]byte{}
	delta := address - original

	if delta == 0 {
		return nil
	}

	read := func(off uint64, cb int) error {
		m.Seek(int64(off), io.SeekStart)
		err := readfully(m, b[0:cb])
		if err != nil {
			return err
		}
		m.Seek(int64(-cb), io.SeekCurrent)
		return nil
	}

	for _, rel := range rels {
		switch rel.Type {
		case ImageRelBasedAbsolute:
			break

		case ImageRelBasedHigh:
			if err := read(rel.Offset, 2); err != nil {
				return err
			}
			o.PutUint16(b[0:2], uint16(uint64(o.Uint16(b[:2]))+(delta>>16)))
			if _, err := m.Write(b[0:2]); err != nil {
				return err
			}
			break

		case ImageRelBasedLow:
			if err := read(rel.Offset, 2); err != nil {
				return err
			}
			o.PutUint16(b[0:2], uint16(uint64(o.Uint16(b[:2]))+(delta>>0)))
			if _, err := m.Write(b[0:2]); err != nil {
				return err
			}
			break

		case ImageRelBasedHighLow:
			if err := read(rel.Offset, 4); err != nil {
				return err
			}
			o.PutUint32(b[0:4], uint32(uint64(o.Uint32(b[:4]))+delta))
			if _, err := m.Write(b[0:4]); err != nil {
				return err
			}
			break

		case ImageRelBasedDir64:
			if err := read(rel.Offset, 8); err != nil {
				return err
			}
			o.PutUint64(b[0:8], uint64(o.Uint64(b[:8]))+delta)
			if _, err := m.Write(b[0:8]); err != nil {
				return err
			}
			break

		// Could use some help for ensuring we have proper support for
		// machine-specific relocations. If you are interested in this
		// use case for some reason, feel free to send PRs. -- john
		case ImageRelBasedMachineSpecific5:
			switch machine {
			// MIPS: JMP reloc
			case ImageFileMachineR3000, ImageFileMachineR3000BE,
				ImageFileMachineR4000, ImageFileMachineR10000,
				ImageFileMachineWCEMIPSv2, ImageFileMachineMIPS16,
				ImageFileMachineMIPSFPU, ImageFileMachineMIPSFPU16:
				return errors.New("MIPS JMP reloc not implemented")
			// ARM: MOV32 reloc
			case ImageFileMachineARM, ImageFileMachineTHUMB,
				ImageFileMachineARMNT:
				return errors.New("ARM MOV32 reloc not implemented")
			// RISC-V: HI20 reloc
			case ImageFileMachineRISCV32, ImageFileMachineRISCV64,
				ImageFileMachineRISCV128:
				return errors.New("RISC-V HI20 reloc not implemented")
			default:
				return fmt.Errorf("unknown machine-specific relocation type %d on machine type %04x", rel.Type, machine)
			}

		case ImageRelBasedMachineSpecific7:
			switch machine {
			// THUMB: MOV32 reloc
			case ImageFileMachineARM, ImageFileMachineTHUMB,
				ImageFileMachineARMNT:
				return errors.New("THUMB MOV32 reloc not implemented")
			// RISC-V: LOW12I reloc
			case ImageFileMachineRISCV32, ImageFileMachineRISCV64,
				ImageFileMachineRISCV128:
				return errors.New("RISC-V LOW12I reloc not implemented")
			default:
				return fmt.Errorf("unknown machine-specific relocation type %d on machine type %04x", rel.Type, machine)
			}

		case ImageRelBasedMachineSpecific8:
			switch machine {
			// RISC-V: LOW12S reloc
			case ImageFileMachineRISCV32, ImageFileMachineRISCV64,
				ImageFileMachineRISCV128:
				return errors.New("RISC-V LOW12S reloc not implemented")
			default:
				return fmt.Errorf("unknown machine-specific relocation type %d on machine type %04x", rel.Type, machine)
			}

		case ImageRelBasedMachineSpecific9:
			switch machine {
			// MIPS: JMP16 reloc
			case ImageFileMachineR3000, ImageFileMachineR3000BE,
				ImageFileMachineR4000, ImageFileMachineR10000,
				ImageFileMachineWCEMIPSv2, ImageFileMachineMIPS16,
				ImageFileMachineMIPSFPU, ImageFileMachineMIPSFPU16:
				return errors.New("MIPS JMP16 reloc not implemented")
			// Itanium: Imm64 reloc
			case ImageFileMachineIA64:
				return errors.New("Itanium Imm64 reloc not implemented")
			default:
				return fmt.Errorf("unknown machine-specific relocation type %d on machine type %04x", rel.Type, machine)
			}
		default:
			return fmt.Errorf("unknown relocation type %d", rel.Type)
		}
	}

	return nil
}
