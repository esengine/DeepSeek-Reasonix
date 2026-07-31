package pe

import (
	"encoding/binary"
	"io"
)

// ExportTable is a table of module exports.
type ExportTable struct {
	symbols  map[string]uint64
	ordinals map[uint16]uint64
}

// Proc returns an exported function address by symbol, or 0 if it is not found.
func (t *ExportTable) Proc(symbol string) (addr uint64) {
	return t.symbols[symbol]
}

// Ordinal returns an exported function address by ordinal, or 0 if it is not found.
func (t *ExportTable) Ordinal(ordinal uint16) (addr uint64) {
	return t.ordinals[ordinal]
}

// LoadExports returns a symbol table.
func LoadExports(m *Module, mem io.ReadWriteSeeker, base uint64) (*ExportTable, error) {
	table := &ExportTable{
		symbols:  map[string]uint64{},
		ordinals: map[uint16]uint64{},
	}

	dir := m.Header.OptionalHeader.DataDirectory[ImageDirectoryEntryExport]
	if dir.Size == 0 {
		return table, nil
	}

	// Load export directory header
	header := ImageExportDirectory{}
	mem.Seek(int64(dir.VirtualAddress), io.SeekStart)
	binary.Read(mem, binary.LittleEndian, &header)

	// Load addresses
	addresses := make([]uint32, header.NumberOfFunctions)
	mem.Seek(int64(header.AddressOfFunctions), io.SeekStart)
	for i := range addresses {
		b := [4]byte{}
		mem.Read(b[:])
		addresses[i] = binary.LittleEndian.Uint32(b[:])
		table.ordinals[uint16(i)] = base + uint64(addresses[i])
	}

	// Load name ordinals
	nameords := make([]uint16, header.NumberOfNames)
	mem.Seek(int64(header.AddressOfNameOrdinals), io.SeekStart)
	for i := range nameords {
		b := [2]byte{}
		mem.Read(b[:])
		nameords[i] = binary.LittleEndian.Uint16(b[:])
	}

	// Load name addresses
	nameaddrs := make([]uint32, header.NumberOfNames)
	mem.Seek(int64(header.AddressOfNames), io.SeekStart)
	for i := range nameaddrs {
		b := [4]byte{}
		mem.Read(b[:])
		nameaddrs[i] = binary.LittleEndian.Uint32(b[:])
	}

	// Load names
	for i, nameaddr := range nameaddrs {
		mem.Seek(int64(nameaddr), io.SeekStart)
		table.symbols[readsz(mem)] = base + uint64(addresses[nameords[i]])
	}

	return table, nil
}
