package winres

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"errors"
	"io"
	"os"
)

type authenticodeHandling int

const (
	// ErrorIfSigned means winres won't patch a signed executable.
	ErrorIfSigned authenticodeHandling = 0
	// RemoveSignature means winres will patch a signed executable,
	// and remove the signature.
	RemoveSignature authenticodeHandling = 1
	// IgnoreSignature means winres will patch a signed executable,
	// and leave the now invalid signature as is.
	IgnoreSignature authenticodeHandling = 2
)

type exeOptions struct {
	forceCheckSum        bool
	authenticodeHandling authenticodeHandling
}

type exeOption func(opt *exeOptions)

// ForceCheckSum forces updating the PE checksum, even when the original file didn't have one
func ForceCheckSum() exeOption {
	return func(opt *exeOptions) {
		opt.forceCheckSum = true
	}
}

// WithAuthenticode allows patching signed executables, either by removing the signature or by ignoring it (and making it wrong)
func WithAuthenticode(handling authenticodeHandling) exeOption {
	return func(opt *exeOptions) {
		opt.authenticodeHandling = handling
	}
}

type peHeaders struct {
	file        pe.FileHeader
	opt         peOptionalHeader
	dirs        []pe.DataDirectory
	sections    []pe.SectionHeader32
	stubLength  int64
	length      int64
	hasChecksum bool
}

const sizeOfSectionHeader = 40

type peOptionalHeader interface {
	getSizeOfInitializedData() uint32
	getSectionAlignment() uint32
	getFileAlignment() uint32
	getNumberOfRvaAndSizes() uint32
	getCheckSum() uint32

	setSizeOfInitializedData(uint32)
	setSizeOfImage(uint32)
	setCheckSum(uint32)
}

type peOptionalHeader32 struct {
	Magic                       uint16
	MajorLinkerVersion          uint8
	MinorLinkerVersion          uint8
	SizeOfCode                  uint32
	SizeOfInitializedData       uint32
	SizeOfUninitializedData     uint32
	AddressOfEntryPoint         uint32
	BaseOfCode                  uint32
	BaseOfData                  uint32
	ImageBase                   uint32
	SectionAlignment            uint32
	FileAlignment               uint32
	MajorOperatingSystemVersion uint16
	MinorOperatingSystemVersion uint16
	MajorImageVersion           uint16
	MinorImageVersion           uint16
	MajorSubsystemVersion       uint16
	MinorSubsystemVersion       uint16
	Win32VersionValue           uint32
	SizeOfImage                 uint32
	SizeOfHeaders               uint32
	CheckSum                    uint32
	Subsystem                   uint16
	DllCharacteristics          uint16
	SizeOfStackReserve          uint32
	SizeOfStackCommit           uint32
	SizeOfHeapReserve           uint32
	SizeOfHeapCommit            uint32
	LoaderFlags                 uint32
	NumberOfRvaAndSizes         uint32
}

func (h *peOptionalHeader32) getSizeOfInitializedData() uint32 {
	return h.SizeOfInitializedData
}

func (h *peOptionalHeader32) getSectionAlignment() uint32 {
	return h.SectionAlignment
}

func (h *peOptionalHeader32) getFileAlignment() uint32 {
	return h.FileAlignment
}

func (h *peOptionalHeader32) getNumberOfRvaAndSizes() uint32 {
	return h.NumberOfRvaAndSizes
}

func (h *peOptionalHeader32) getCheckSum() uint32 {
	return h.CheckSum
}

func (h *peOptionalHeader32) setSizeOfInitializedData(s uint32) {
	h.SizeOfInitializedData = s
}

func (h *peOptionalHeader32) setSizeOfImage(s uint32) {
	h.SizeOfImage = s
}

func (h *peOptionalHeader32) setCheckSum(c uint32) {
	h.CheckSum = c
}

type peOptionalHeader64 struct {
	Magic                       uint16
	MajorLinkerVersion          uint8
	MinorLinkerVersion          uint8
	SizeOfCode                  uint32
	SizeOfInitializedData       uint32
	SizeOfUninitializedData     uint32
	AddressOfEntryPoint         uint32
	BaseOfCode                  uint32
	ImageBase                   uint64
	SectionAlignment            uint32
	FileAlignment               uint32
	MajorOperatingSystemVersion uint16
	MinorOperatingSystemVersion uint16
	MajorImageVersion           uint16
	MinorImageVersion           uint16
	MajorSubsystemVersion       uint16
	MinorSubsystemVersion       uint16
	Win32VersionValue           uint32
	SizeOfImage                 uint32
	SizeOfHeaders               uint32
	CheckSum                    uint32
	Subsystem                   uint16
	DllCharacteristics          uint16
	SizeOfStackReserve          uint64
	SizeOfStackCommit           uint64
	SizeOfHeapReserve           uint64
	SizeOfHeapCommit            uint64
	LoaderFlags                 uint32
	NumberOfRvaAndSizes         uint32
}

func (h *peOptionalHeader64) getSizeOfInitializedData() uint32 {
	return h.SizeOfInitializedData
}

func (h *peOptionalHeader64) getSectionAlignment() uint32 {
	return h.SectionAlignment
}

func (h *peOptionalHeader64) getFileAlignment() uint32 {
	return h.FileAlignment
}

func (h *peOptionalHeader64) getNumberOfRvaAndSizes() uint32 {
	return h.NumberOfRvaAndSizes
}

func (h *peOptionalHeader64) getCheckSum() uint32 {
	return h.CheckSum
}

func (h *peOptionalHeader64) setSizeOfInitializedData(s uint32) {
	h.SizeOfInitializedData = s
}

func (h *peOptionalHeader64) setSizeOfImage(s uint32) {
	h.SizeOfImage = s
}

func (h *peOptionalHeader64) setCheckSum(c uint32) {
	h.CheckSum = c
}

func extractRSRCSection(r io.ReadSeeker) ([]byte, uint32, error) {
	r.Seek(0, io.SeekStart)

	fileSize := getSeekerSize(r)

	h, err := readPEHeaders(r)
	if err != nil {
		return nil, 0, err
	}

	if h.dirs[pe.IMAGE_DIRECTORY_ENTRY_RESOURCE].VirtualAddress == 0 && h.dirs[pe.IMAGE_DIRECTORY_ENTRY_RESOURCE].Size == 0 {
		return nil, 0, ErrNoResources
	}

	var sec *pe.SectionHeader32
	for i := range h.sections {
		if h.sections[i].VirtualAddress == h.dirs[pe.IMAGE_DIRECTORY_ENTRY_RESOURCE].VirtualAddress {
			sec = &h.sections[i]
			break
		}
	}
	if sec == nil {
		return nil, 0, errors.New(errRSRCNotFound)
	}

	if int64(sec.PointerToRawData)+int64(sec.SizeOfRawData) > fileSize {
		return nil, 0, errors.New(errSectionTooFar)
	}
	data := make([]byte, sec.SizeOfRawData)

	r.Seek(int64(sec.PointerToRawData), io.SeekStart)
	err = readFull(r, data)
	if err != nil {
		return nil, 0, err
	}

	return data, sec.VirtualAddress, nil
}

type peWriter struct {
	h        *peHeaders
	rsrcData []byte
	rsrcHdr  *pe.SectionHeader32
	relocHdr *pe.SectionHeader32
	src      struct {
		r          io.ReadSeeker
		fileSize   int64
		sigSize    int64 // size of a code signature we'd want to skip (only if it is at the end of the file)
		dataOffset uint32
		dataEnd    uint32
		virtEnd    uint32
		rsrcEnd    int64
	}
}

func replaceRSRCSection(dst io.Writer, src io.ReadSeeker, rsrcData []byte, reloc []int, options exeOptions) error {
	src.Seek(0, io.SeekStart)

	pew, err := preparePEWriter(src, rsrcData, options.authenticodeHandling)
	if err != nil {
		return err
	}

	pew.applyReloc(reloc)

	if options.forceCheckSum || pew.h.hasChecksum {
		c := peCheckSum{}
		pew.writeEXE(&c)
		pew.h.opt.setCheckSum(c.Sum())
	}

	return pew.writeEXE(dst)
}

func preparePEWriter(src io.ReadSeeker, rsrcData []byte, sigHandling authenticodeHandling) (*peWriter, error) {
	var (
		pew peWriter
		err error
	)

	pew.src.r = src
	pew.rsrcData = rsrcData

	pew.src.fileSize = getSeekerSize(src)

	pew.h, err = readPEHeaders(src)
	if err != nil {
		return nil, err
	}

	if len(pew.h.dirs) > pe.IMAGE_DIRECTORY_ENTRY_SECURITY && pew.h.dirs[pe.IMAGE_DIRECTORY_ENTRY_SECURITY].VirtualAddress > 0 {
		switch sigHandling {
		case RemoveSignature:
			entry := pew.h.dirs[pe.IMAGE_DIRECTORY_ENTRY_SECURITY]
			pew.h.dirs[pe.IMAGE_DIRECTORY_ENTRY_SECURITY] = pe.DataDirectory{}
			// The certificate entry actually contains a raw data offset, not a virtual address.
			// https://docs.microsoft.com/en-us/windows/win32/debug/pe-format#the-attribute-certificate-table-image-only
			if int64(entry.VirtualAddress)+int64(entry.Size) == pew.src.fileSize {
				pew.src.sigSize = pew.src.fileSize - int64(entry.VirtualAddress)
			}
		case IgnoreSignature:
		default:
			return nil, ErrSignedPE
		}
	}

	err = pew.fillSectionsInfo()
	if err != nil {
		return nil, err
	}

	if pew.src.fileSize < int64(pew.src.dataEnd) {
		return nil, io.ErrUnexpectedEOF
	}
	if pew.rsrcHdr == nil && (int64(pew.src.dataOffset) < pew.h.length+sizeOfSectionHeader || pew.h.file.NumberOfSections == 0xFFFF) {
		return nil, errors.New(errNoRoomForRSRC)
	}

	if pew.requiresNewSection() {
		// Play it safe, abandon the existing .rsrc section and create a new one.
		pew.rsrcHdr.Name = [8]byte{'o', 'l', 'd', '.', 'r', 's', 'r', 'c'}
		pew.rsrcHdr = nil
	}

	pew.updateHeaders()

	return &pew, nil
}

func (pew *peWriter) fillSectionsInfo() error {
	pew.src.dataOffset = 0xFFFFFFFF
	pew.src.rsrcEnd = pew.src.fileSize

	for i := range pew.h.sections {
		if pew.h.sections[i].VirtualAddress == pew.h.dirs[pe.IMAGE_DIRECTORY_ENTRY_RESOURCE].VirtualAddress {
			if pew.rsrcHdr != nil {
				return errors.New(errRSRCTwice)
			}
			pew.rsrcHdr = &pew.h.sections[i]
			pew.src.rsrcEnd = int64(pew.roundRaw(pew.rsrcHdr.PointerToRawData + pew.rsrcHdr.SizeOfRawData))
		}
		if pew.h.sections[i].VirtualAddress == pew.h.dirs[pe.IMAGE_DIRECTORY_ENTRY_BASERELOC].VirtualAddress {
			if pew.relocHdr != nil {
				return errors.New(errRelocTwice)
			}
			pew.relocHdr = &pew.h.sections[i]
		}
	}

	for i := range pew.h.sections {
		if pew.h.sections[i].VirtualAddress+pew.h.sections[i].VirtualSize > pew.src.virtEnd {
			pew.src.virtEnd = pew.h.sections[i].VirtualAddress + pew.h.sections[i].VirtualSize
		}
		if pew.h.sections[i].Characteristics&pe.IMAGE_SCN_CNT_UNINITIALIZED_DATA != 0 {
			// Exclude sections containing uninitialized data from file offset
			// calculations, as they do not occupy any data in the file.
			continue
		}
		if pew.h.sections[i].PointerToRawData < pew.src.dataOffset {
			pew.src.dataOffset = pew.h.sections[i].PointerToRawData
		}
		if pew.h.sections[i].PointerToRawData+pew.h.sections[i].SizeOfRawData > pew.src.dataEnd {
			pew.src.dataEnd = pew.h.sections[i].PointerToRawData + pew.h.sections[i].SizeOfRawData
		}
	}
	pew.src.virtEnd = pew.roundVirt(pew.src.virtEnd)

	return nil
}

func (pew *peWriter) requiresNewSection() bool {
	if pew.rsrcHdr == nil {
		return false
	}
	endOfRSRC := pew.roundVirt(pew.rsrcHdr.VirtualAddress + pew.rsrcHdr.VirtualSize)
	if endOfRSRC >= pew.src.virtEnd {
		return false
	}
	if pew.relocHdr.VirtualAddress == endOfRSRC &&
		pew.roundVirt(pew.relocHdr.VirtualAddress+pew.relocHdr.VirtualSize) >= pew.src.virtEnd {
		return false
	}

	// From here, we should not shift data after the existing .rsrc section
	if pew.rsrcHdr.SizeOfRawData >= uint32(len(pew.rsrcData)) {
		// The .rsrc section won't grow, so we only have to ensure it won't shrink too much either
		buf := make([]byte, pew.rsrcHdr.SizeOfRawData)
		copy(buf, pew.rsrcData)
		pew.rsrcData = buf
		return false
	}

	return true
}

func (pew *peWriter) updateHeaders() {
	var (
		rsrcLen     = uint32(len(pew.rsrcData))
		lastSection *pe.SectionHeader32
		oldSize     uint32
		virtDelta   uint32
	)

	if pew.rsrcHdr == nil {
		// Add .rsrc section
		pew.h.sections = append(pew.h.sections, pe.SectionHeader32{
			Name:             [8]uint8{'.', 'r', 's', 'r', 'c'},
			VirtualSize:      rsrcLen,
			VirtualAddress:   pew.roundVirt(pew.src.virtEnd),
			SizeOfRawData:    pew.roundRaw(uint32(len(pew.rsrcData))),
			PointerToRawData: pew.roundRaw(pew.src.dataEnd),
			Characteristics:  _IMAGE_SCN_MEM_READ | _IMAGE_SCN_CNT_INITIALIZED_DATA,
		})
		pew.rsrcHdr = &pew.h.sections[len(pew.h.sections)-1]
		pew.h.file.NumberOfSections++
		pew.h.length += sizeOfSectionHeader
		lastSection = pew.rsrcHdr
		pew.h.opt.setSizeOfInitializedData(pew.h.opt.getSizeOfInitializedData() + rsrcLen)
	} else {
		oldSize = pew.rsrcHdr.SizeOfRawData
		virtDelta = pew.roundVirt(rsrcLen) - pew.roundVirt(pew.rsrcHdr.VirtualSize)
		rawDelta := pew.roundRaw(rsrcLen) - pew.roundRaw(pew.rsrcHdr.SizeOfRawData)
		pew.rsrcHdr.VirtualSize = rsrcLen
		pew.rsrcHdr.SizeOfRawData = pew.roundRaw(rsrcLen)
		lastSection = pew.rsrcHdr
		for i := range pew.h.sections {
			if pew.h.sections[i].VirtualAddress > pew.rsrcHdr.VirtualAddress {
				pew.h.sections[i].VirtualAddress += virtDelta
				if pew.h.sections[i].VirtualAddress > lastSection.VirtualAddress {
					lastSection = &pew.h.sections[i]
				}
			}
			if pew.h.sections[i].PointerToRawData > pew.rsrcHdr.PointerToRawData {
				pew.h.sections[i].PointerToRawData += rawDelta
			}
		}
	}

	if pew.h.dirs[pe.IMAGE_DIRECTORY_ENTRY_SECURITY].VirtualAddress >= pew.src.dataEnd {
		pew.h.dirs[pe.IMAGE_DIRECTORY_ENTRY_SECURITY].VirtualAddress += lastSection.PointerToRawData + lastSection.SizeOfRawData - pew.src.dataEnd
	}

	pew.h.dirs[pe.IMAGE_DIRECTORY_ENTRY_RESOURCE].VirtualAddress = pew.rsrcHdr.VirtualAddress
	pew.h.dirs[pe.IMAGE_DIRECTORY_ENTRY_RESOURCE].Size = rsrcLen
	for i := range pew.h.dirs {
		if i != pe.IMAGE_DIRECTORY_ENTRY_SECURITY && pew.h.dirs[i].VirtualAddress > pew.rsrcHdr.VirtualAddress {
			pew.h.dirs[i].VirtualAddress += virtDelta
		}
	}

	pew.h.opt.setCheckSum(0)
	pew.h.opt.setSizeOfImage(lastSection.VirtualAddress + pew.roundVirt(lastSection.VirtualSize))
	pew.h.opt.setSizeOfInitializedData(pew.h.opt.getSizeOfInitializedData() - oldSize + pew.rsrcHdr.SizeOfRawData)
}

func (pew *peWriter) roundRaw(p uint32) uint32 {
	a := pew.h.opt.getFileAlignment()
	x := p + a - 1
	return x - x%a
}

func (pew *peWriter) roundVirt(p uint32) uint32 {
	a := pew.h.opt.getSectionAlignment()
	x := p + a - 1
	return x - x%a
}

func (pew *peWriter) applyReloc(reloc []int) {
	for _, o := range reloc {
		addr := uint32(pew.rsrcData[o+3])<<24 |
			uint32(pew.rsrcData[o+2])<<16 |
			uint32(pew.rsrcData[o+1])<<8 |
			uint32(pew.rsrcData[o])
		addr += pew.rsrcHdr.VirtualAddress
		pew.rsrcData[o+3] = uint8(addr >> 24)
		pew.rsrcData[o+2] = uint8(addr >> 16)
		pew.rsrcData[o+1] = uint8(addr >> 8)
		pew.rsrcData[o] = uint8(addr)
	}
}

func (pew *peWriter) writeEXE(w io.Writer) error {
	var err error

	_, err = pew.src.r.Seek(0, io.SeekStart)
	if err != nil {
		return err
	}

	// MS-DOS Stub + PE signature
	_, err = io.CopyN(w, pew.src.r, pew.h.stubLength+4)
	if err != nil {
		return err
	}

	// Headers
	err = binary.Write(w, binary.LittleEndian, &pew.h.file)
	if err != nil {
		return err
	}
	err = binary.Write(w, binary.LittleEndian, pew.h.opt)
	if err != nil {
		return err
	}
	err = binary.Write(w, binary.LittleEndian, pew.h.dirs)
	if err != nil {
		return err
	}
	err = binary.Write(w, binary.LittleEndian, pew.h.sections)
	if err != nil {
		return err
	}
	err = writeBlank(w, int64(pew.src.dataOffset)-pew.h.length)
	if err != nil {
		return err
	}
	_, err = pew.src.r.Seek(int64(pew.src.dataOffset), io.SeekStart)
	if err != nil {
		return err
	}

	// Sections before .rsrc
	end := int64(pew.src.dataEnd)
	if int64(pew.rsrcHdr.PointerToRawData) < end {
		end = int64(pew.rsrcHdr.PointerToRawData)
	}
	_, err = io.CopyN(w, pew.src.r, end-int64(pew.src.dataOffset))
	if err != nil {
		return err
	}
	err = writeBlank(w, int64(pew.rsrcHdr.PointerToRawData)-end)
	if err != nil {
		return err
	}

	// .rsrc
	_, err = w.Write(pew.rsrcData)
	if err != nil {
		return err
	}
	err = writeBlank(w, int64(pew.rsrcHdr.SizeOfRawData)-int64(len(pew.rsrcData)))
	if err != nil {
		return err
	}

	// Remainder
	_, err = pew.src.r.Seek(pew.src.rsrcEnd, io.SeekStart)
	if err != nil {
		return err
	}
	_, err = io.CopyN(w, pew.src.r, pew.src.fileSize-pew.src.sigSize-pew.src.rsrcEnd)
	if err != nil {
		return err
	}

	return nil
}

func writeBlank(w io.Writer, length int64) error {
	if length <= 0 {
		return nil
	}

	if ws, ok := w.(io.WriteSeeker); ok {
		ws.Seek(length-1, io.SeekCurrent)
		var b [1]byte
		_, err := w.Write(b[:])
		if err != nil {
			return err
		}
		return nil
	}

	const bufLen = 0x100
	var b [bufLen]byte
	for length > 0 {
		l := length
		if l > bufLen {
			l = bufLen
		}
		_, err := w.Write(b[:l])
		if err != nil {
			return err
		}
		length -= l
	}
	return nil
}

func readPEOffset(r io.Reader) (int64, error) {
	stubHead := make([]byte, 0x40)

	err := readFull(r, stubHead)
	if err != nil {
		return 0, err
	}

	if string(stubHead[:2]) != "MZ" {
		return 0, errors.New(errNotPEImage)
	}

	return int64(stubHead[0x3F])<<24 | int64(stubHead[0x3E])<<16 | int64(stubHead[0x3D])<<8 | int64(stubHead[0x3C]), nil
}

func readPEHeaders(r io.ReadSeeker) (*peHeaders, error) {
	var (
		h   peHeaders
		err error
	)

	h.stubLength, err = readPEOffset(r)
	if err != nil {
		return nil, err
	}
	r.Seek(h.stubLength, io.SeekStart)

	var sig [4]byte
	err = readFull(r, sig[:])
	if err != nil {
		return nil, err
	}
	if sig != [4]byte{'P', 'E'} {
		return nil, errors.New(errNotPEImage)
	}

	err = binaryRead(r, &h.file)
	if err != nil {
		return nil, err
	}

	optHdr := make([]byte, h.file.SizeOfOptionalHeader)
	err = readFull(r, optHdr)
	if err != nil {
		return nil, err
	}
	if optHdr[0] != 11 {
		return nil, errors.New(errUnknownPE)
	}
	switch optHdr[1] {
	case 1:
		h.opt = &peOptionalHeader32{}
	case 2:
		h.opt = &peOptionalHeader64{}
	default:
		return nil, errors.New(errUnknownPE)
	}
	optRead := bytes.NewReader(optHdr)
	err = binaryRead(optRead, h.opt)
	if err != nil {
		return nil, err
	}
	numDirs := int(h.opt.getNumberOfRvaAndSizes())
	if numDirs < 6 {
		return nil, errors.New(errUnknownPE)
	}
	if int(h.file.SizeOfOptionalHeader) != binary.Size(h.opt)+numDirs*8 {
		return nil, errors.New(errNotPEImage)
	}

	h.dirs = make([]pe.DataDirectory, numDirs)
	binaryRead(optRead, &h.dirs)

	h.sections = make([]pe.SectionHeader32, h.file.NumberOfSections, h.file.NumberOfSections+1)
	err = binaryRead(r, &h.sections)
	if err != nil {
		return nil, err
	}

	h.hasChecksum = h.opt.getCheckSum() != 0
	h.length, err = r.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}
	return &h, nil
}

func getSeekerSize(r io.ReadSeeker) int64 {
	switch r := r.(type) {
	case *os.File:
		stat, err := r.Stat()
		if err == nil {
			return stat.Size()
		}
	case interface{ Size() int64 }:
		return r.Size()
	}
	pos, _ := r.Seek(0, io.SeekCurrent)
	size, _ := r.Seek(0, io.SeekEnd)
	r.Seek(pos, io.SeekStart)
	return size
}
