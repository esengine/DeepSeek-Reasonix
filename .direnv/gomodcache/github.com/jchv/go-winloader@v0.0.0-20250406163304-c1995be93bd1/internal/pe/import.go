package pe

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/jchv/go-winloader/internal/loader"
)

// LinkModule links a PE module in-memory.
func LinkModule(m *Module, mem io.ReadWriteSeeker, ldr loader.Loader) error {
	dir := m.Header.OptionalHeader.DataDirectory[ImageDirectoryEntryImport]
	if dir.Size == 0 {
		return nil
	}

	// Determine pointer size based on whether we're PE32 or PE64.
	psize := 4
	if m.IsPE64 {
		psize = 8
	}

	// Load import descriptors
	descs := []ImageImportDescriptor{}
	mem.Seek(int64(dir.VirtualAddress), io.SeekStart)
	for {
		desc := ImageImportDescriptor{}
		binary.Read(mem, binary.LittleEndian, &desc)

		if desc.Name == 0 {
			break
		}

		descs = append(descs, desc)
	}

	// Load modules.
	for _, desc := range descs {
		thunk := int64(desc.OriginalFirstThunk)
		iat := int64(desc.FirstThunk)
		if thunk == 0 {
			thunk = iat
		}

		// Read module name
		mem.Seek(int64(desc.Name), io.SeekStart)

		// Load library
		libname := readsz(mem)
		lib, err := ldr.Load(libname)
		if err != nil {
			return err
		}

		// Read thunk addrs
		b := [8]byte{}
		thunks := []uint64{}
		mem.Seek(thunk, io.SeekStart)
		for {
			mem.Read(b[:psize])
			thunk := binary.LittleEndian.Uint64(b[:])
			if thunk == 0 {
				break
			}
			thunks = append(thunks, thunk)
		}

		// Resolve thunks
		resolved := []uint64{}
		for _, thunk := range thunks {
			thunkord := int64(-1)
			if (m.IsPE64 && thunk&0x8000000000000000 != 0) || (!m.IsPE64 && thunk&0x80000000 != 0) {
				thunkord = int64(thunk & 0xFFFF)
			}
			if thunkord != -1 {
				// Import by ordinal
				if proc := lib.Ordinal(uint64(thunkord)); proc != nil {
					resolved = append(resolved, proc.Addr())
				} else {
					return fmt.Errorf("could not resolve ordinal %d in module %q", thunkord, libname)
				}
			} else {
				// Read name
				mem.Seek(int64(thunk+2), io.SeekStart)
				fnname := readsz(mem)

				// Import by name
				if proc := lib.Proc(fnname); proc != nil {
					resolved = append(resolved, proc.Addr())
				} else {
					return fmt.Errorf("could not resolve symbol %q in module %q", fnname, libname)
				}
			}
		}

		// Write resolved IAT
		mem.Seek(iat, io.SeekStart)
		for _, fn := range resolved {
			binary.LittleEndian.PutUint64(b[:], uint64(fn))
			mem.Write(b[:psize])
		}
	}

	return nil
}
