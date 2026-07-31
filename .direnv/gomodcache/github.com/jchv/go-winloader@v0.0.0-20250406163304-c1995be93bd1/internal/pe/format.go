package pe

// CONSTANTS / MAGIC NUMBERS

// MZSignature is the signature of the MZ format. This is the value of the
// Signature field in ImageDOSHeader.
var MZSignature = [2]byte{'M', 'Z'}

// PESignature is the signature of the PE format. This is the value of the
// Signature field in ImageNTHeaders32 and ImageNTHeaders64.
var PESignature = [4]byte{'P', 'E', 0, 0}

// Enumeration of magic numbers
const (
	// ImageNTOptionalHeader32Magic is the magic number for 32-bit optional
	// header (ImageOptionalHeader32)
	ImageNTOptionalHeader32Magic = 0x010b

	// ImageNTOptionalHeader64Magic is the magic number for 64-bit optional
	// header (ImageOptionalHeader64)
	ImageNTOptionalHeader64Magic = 0x020b
)

// Enumeration of structure lengths.
const (
	// SizeOfImageDOSHeader is the on-disk size of the ImageDOSHeader
	// structure.
	SizeOfImageDOSHeader = 64

	// SizeOfImageFileHeader is the on-disk size of the ImageFileHeader
	// structure.
	SizeOfImageFileHeader = 20

	// SizeOfImageOptionalHeader32 is the on-disk size of the
	// ImageOptionalHeader32 structure.
	SizeOfImageOptionalHeader32 = 224

	// SizeOfImageOptionalHeader64 is the on-disk size of the
	// ImageOptionalHeader64 structure.
	SizeOfImageOptionalHeader64 = 240

	// SizeOfImageNTHeaders32 is the on-disk size of the ImageNTHeaders32
	// structure.
	SizeOfImageNTHeaders32 = 248

	// SizeOfImageNTHeaders64 is the on-disk size of the ImageNTHeaders64
	// structure.
	SizeOfImageNTHeaders64 = 264

	// SizeOfImageDataDirectory is the on-disk size of the ImageDataDirectory
	// structure.
	SizeOfImageDataDirectory = 8
)

// Enumeration of useful field offsets.
const (
	// OffsetOfOptionalHeaderFromNTHeader is the offset from the start of
	// the NT header to the optional header magic value. This is helpful for
	// determining if the PE file is PE32 or PE64.
	OffsetOfOptionalHeaderFromNTHeader = 0x18
)

// Enumeration of fixed-size array lengths in PE
const (
	// NumDirectoryEntries specifies the number of data directory entries.
	NumDirectoryEntries = 16

	// SectionNameLength is the size of a section short name.
	SectionNameLength = 8
)

// Enumeration of known Windows Loader limits. (Some of these may not be
// imposed by the format itself and purely by Windows runtime.)
const (
	// MaxNumSections specifies the maximum number of sections that are
	// allowed. This is imposed by Windows Loader.
	MaxNumSections = 96
)

// ENUMERATION VALUES

// Enumeration of machine values for the file header.
const (
	ImageFileMachineUnknown    = 0x0000
	ImageFileMachineTargetHost = 0x0001
	ImageFileMachinei386       = 0x014c
	ImageFileMachineR3000BE    = 0x0160
	ImageFileMachineR3000      = 0x0162
	ImageFileMachineR4000      = 0x0166
	ImageFileMachineR10000     = 0x0168
	ImageFileMachineWCEMIPSv2  = 0x0169
	ImageFileMachineAlpha      = 0x0184
	ImageFileMachineSH3        = 0x01a2
	ImageFileMachineSH3DSP     = 0x01a3
	ImageFileMachineSH3E       = 0x01a4
	ImageFileMachineSH4        = 0x01a6
	ImageFileMachineSH5        = 0x01a8
	ImageFileMachineARM        = 0x01c0
	ImageFileMachineTHUMB      = 0x01c2
	ImageFileMachineARMNT      = 0x01c4
	ImageFileMachineAM33       = 0x01d3
	ImageFileMachinePowerPC    = 0x01F0
	ImageFileMachinePowerPCFP  = 0x01f1
	ImageFileMachineIA64       = 0x0200
	ImageFileMachineMIPS16     = 0x0266
	ImageFileMachineAlpha64    = 0x0284
	ImageFileMachineMIPSFPU    = 0x0366
	ImageFileMachineMIPSFPU16  = 0x0466
	ImageFileMachineAXP64      = ImageFileMachineAlpha64
	ImageFileMachineTricore    = 0x0520
	ImageFileMachineCEF        = 0x00CE
	ImageFileMachineEBC        = 0x0EBC
	ImageFileMachineAMD64      = 0x8664
	ImageFileMachineM32R       = 0x9041
	ImageFileMachineARM64      = 0xAA64
	ImageFileMachineCEE        = 0x0C0E
	ImageFileMachineRISCV32    = 0x5032
	ImageFileMachineRISCV64    = 0x5064
	ImageFileMachineRISCV128   = 0x5128
)

// Enumeration of charateristics values for the file header.
const (
	ImageFileRelocsStripped       = 0x0001
	ImageFileExecutableImage      = 0x0002
	ImageFileLineNumsStripped     = 0x0004
	ImageFileLocalSymsStripped    = 0x0008
	ImageFileAggressiveWSTrim     = 0x0010
	ImageFileLargeAddressAware    = 0x0020
	ImageFileBytesReversedLo      = 0x0080
	ImageFile32BitMachine         = 0x0100
	ImageFileDebugStripped        = 0x0200
	ImageFileRemovableRunFromSwap = 0x0400
	ImageFileNetRunFromSwap       = 0x0800
	ImageFileSystem               = 0x1000
	ImageFileDLL                  = 0x2000
	ImageFileUPSystemOnly         = 0x4000
	ImageFileBytesReversedHi      = 0x8000
)

// Enumeration of image subsystem values.
const (
	ImageSubsystemUnknown                = 0
	ImageSubsystemNative                 = 1
	ImageSubsystemWindowsGUI             = 2
	ImageSubsystemWindowsCUI             = 3
	ImageSubsystemOS2CUI                 = 5
	ImageSubsystemPOSIXCUI               = 7
	ImageSubsystemNativeWindows          = 8
	ImageSubsystemWindowsCEGUI           = 9
	ImageSubsystemEFIApplication         = 10
	ImageSubsystemEFIBootServiceDriver   = 11
	ImageSubsystemEFIRuntimeDriver       = 12
	ImageSubsystemEFIROM                 = 13
	ImageSubsystemXBox                   = 14
	ImageSubsystemWindowsBootApplication = 16
	ImageSubsystemXBoxCodeCatalog        = 17
)

// Enumeration of DLL characteristics values.
const (
	ImageDLLCharacteristicsHighEntropyVA       = 0x0020
	ImageDLLCharacteristicsDynamicBase         = 0x0040
	ImageDLLCharacteristicsForceIntegrity      = 0x0080
	ImageDLLCharacteristicsNXCompat            = 0x0100
	ImageDLLCharacteristicsNoIsolation         = 0x0200
	ImageDLLCharacteristicsNoSEH               = 0x0400
	ImageDLLCharacteristicsNoBind              = 0x0800
	ImageDLLCharacteristicsAppContainer        = 0x1000
	ImageDLLCharacteristicsWDMDriver           = 0x2000
	ImageDLLCharacteristicsGuardCF             = 0x4000
	ImageDLLCharacteristicsTerminalServerAware = 0x8000
)

// Enumeration of image directory entry indexes. These represent indices into
// the data directory array of the optional header.
const (
	ImageDirectoryEntryExport        = 0
	ImageDirectoryEntryImport        = 1
	ImageDirectoryEntryResource      = 2
	ImageDirectoryEntryException     = 3
	ImageDirectoryEntrySecurity      = 4
	ImageDirectoryEntryBaseReloc     = 5
	ImageDirectoryEntryDebug         = 6
	ImageDirectoryEntryCopyright     = 7
	ImageDirectoryEntryArchitecture  = 7
	ImageDirectoryEntryGlobalPtr     = 8
	ImageDirectoryEntryTLS           = 9
	ImageDirectoryEntryLoadConfig    = 10
	ImageDirectoryEntryBoundImport   = 11
	ImageDirectoryEntryIAT           = 12
	ImageDirectoryEntryDelayImport   = 13
	ImageDirectoryEntryCOMDescriptor = 14
)

// Enumeration of image section characteristics.
const (
	ImageSectionCharacteristicsNoPad                     = 0x00000008
	ImageSectionCharacteristicsContainsCode              = 0x00000020
	ImageSectionCharacteristicsContainsInitializedData   = 0x00000040
	ImageSectionCharacteristicsContainsUninitailizedData = 0x00000080
	ImageSectionCharacteristicsLinkOther                 = 0x00000100
	ImageSectionCharacteristicsLinkInfo                  = 0x00000200
	ImageSectionCharacteristicsLinkRemove                = 0x00000800
	ImageSectionCharacteristicsLinkCOMDAT                = 0x00001000
	ImageSectionCharacteristicsNoDeferSpecExc            = 0x00004000
	ImageSectionCharacteristicsGPRel                     = 0x00008000
	ImageSectionCharacteristicsMemoryFarData             = 0x00008000
	ImageSectionCharacteristicsMemoryPurgeable           = 0x00020000
	ImageSectionCharacteristicsMemory16Bit               = 0x00020000
	ImageSectionCharacteristicsMemoryLocked              = 0x00040000
	ImageSectionCharacteristicsMemoryPreload             = 0x00080000
	ImageSectionCharacteristicsAlign1Bytes               = 0x00100000
	ImageSectionCharacteristicsAlign2Bytes               = 0x00200000
	ImageSectionCharacteristicsAlign4Bytes               = 0x00300000
	ImageSectionCharacteristicsAlign8Bytes               = 0x00400000
	ImageSectionCharacteristicsAlign16Bytes              = 0x00500000
	ImageSectionCharacteristicsAlign32Bytes              = 0x00600000
	ImageSectionCharacteristicsAlign64Bytes              = 0x00700000
	ImageSectionCharacteristicsAlign128Bytes             = 0x00800000
	ImageSectionCharacteristicsAlign256Bytes             = 0x00900000
	ImageSectionCharacteristicsAlign512Bytes             = 0x00A00000
	ImageSectionCharacteristicsAlign1024Bytes            = 0x00B00000
	ImageSectionCharacteristicsAlign2048Bytes            = 0x00C00000
	ImageSectionCharacteristicsAlign4096Bytes            = 0x00D00000
	ImageSectionCharacteristicsAlign8192Bytes            = 0x00E00000
	ImageSectionCharacteristicsAlignMask                 = 0x00F00000
	ImageSectionCharacteristicsLinkNumRelocOverflow      = 0x01000000
	ImageSectionCharacteristicsMemoryDiscardable         = 0x02000000
	ImageSectionCharacteristicsMemoryNotCached           = 0x04000000
	ImageSectionCharacteristicsMemoryNotPaged            = 0x08000000
	ImageSectionCharacteristicsMemoryShared              = 0x10000000
	ImageSectionCharacteristicsMemoryExecute             = 0x20000000
	ImageSectionCharacteristicsMemoryRead                = 0x40000000
	ImageSectionCharacteristicsMemoryWrite               = 0x80000000
)

// Enumeration of TLS characteristics.
const (
	ImageSectionTLSCharacteristicsScaleIndex = 0x00000001
)

// Enumeration of relocation types.
const (
	ImageRelBasedAbsolute         = 0
	ImageRelBasedHigh             = 1
	ImageRelBasedLow              = 2
	ImageRelBasedHighLow          = 3
	ImageRelBasedHighAdj          = 4
	ImageRelBasedMachineSpecific5 = 5
	ImageRelBasedReserved         = 6
	ImageRelBasedMachineSpecific7 = 7
	ImageRelBasedMachineSpecific8 = 8
	ImageRelBasedMachineSpecific9 = 9
	ImageRelBasedDir64            = 10
)

// ImageDOSHeader is the structure of the DOS MZ Executable format. All PE
// files contain at least a valid stub DOS MZ executable at the top; the PE
// format itself starts at the address specified by NewHeaderAddr.
type ImageDOSHeader struct {
	Signature     [2]byte
	LastPageBytes uint16
	CountPages    uint16
	CountRelocs   uint16
	HeaderLen     uint16
	MinAlloc      uint16
	MaxAlloc      uint16
	InitialSS     uint16
	InitialSP     uint16
	Checksum      uint16
	InitialIP     uint16
	InitialCS     uint16
	RelocAddr     uint16
	OverlayNum    uint16
	Reserved      [4]uint16
	OEMID         uint16
	OEMInfo       uint16
	Reserved2     [10]uint16
	NewHeaderAddr uint32
}

// ImageFileHeader contains some of the basic attributes about the PE/COFF
// file, including the number of sections and the machine type.
type ImageFileHeader struct {
	Machine              uint16
	NumberOfSections     uint16
	TimeDateStamp        uint32
	PointerToSymbolTable uint32
	NumberOfSymbols      uint32
	SizeOfOptionalHeader uint16
	Characteristics      uint16
}

// ImageOptionalHeader32 contains the optional header for 32-bit PE images. It
// is only 'optional' in the sense that not all PE/COFF binaries have it,
// however it is required for executables and DLLs.
type ImageOptionalHeader32 struct {
	Magic                   uint16
	MajorLinkerVersion      uint8
	MinorLinkerVersion      uint8
	SizeOfCode              uint32
	SizeOfInitializedData   uint32
	SizeOfUninitializedData uint32
	AddressOfEntryPoint     uint32
	BaseOfCode              uint32
	BaseOfData              uint32

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
	DataDirectory               [NumDirectoryEntries]ImageDataDirectory
}

// ImageOptionalHeader64 contains the optional header for 64-bit PE images.
type ImageOptionalHeader64 struct {
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
	DataDirectory               [NumDirectoryEntries]ImageDataDirectory
}

// To64 converts the ImageOptionalHeader32 to an ImageOptionalHeader64.
func (i ImageOptionalHeader32) To64() ImageOptionalHeader64 {
	return ImageOptionalHeader64{
		Magic:                       i.Magic,
		MajorLinkerVersion:          i.MajorLinkerVersion,
		MinorLinkerVersion:          i.MinorLinkerVersion,
		SizeOfCode:                  i.SizeOfCode,
		SizeOfInitializedData:       i.SizeOfInitializedData,
		SizeOfUninitializedData:     i.SizeOfUninitializedData,
		AddressOfEntryPoint:         i.AddressOfEntryPoint,
		BaseOfCode:                  i.BaseOfCode,
		ImageBase:                   uint64(i.ImageBase),
		SectionAlignment:            i.SectionAlignment,
		FileAlignment:               i.FileAlignment,
		MajorOperatingSystemVersion: i.MajorOperatingSystemVersion,
		MinorOperatingSystemVersion: i.MinorOperatingSystemVersion,
		MajorImageVersion:           i.MajorImageVersion,
		MinorImageVersion:           i.MinorImageVersion,
		MajorSubsystemVersion:       i.MajorSubsystemVersion,
		MinorSubsystemVersion:       i.MinorSubsystemVersion,
		Win32VersionValue:           i.Win32VersionValue,
		SizeOfImage:                 i.SizeOfImage,
		SizeOfHeaders:               i.SizeOfHeaders,
		CheckSum:                    i.CheckSum,
		Subsystem:                   i.Subsystem,
		DllCharacteristics:          i.DllCharacteristics,
		SizeOfStackReserve:          uint64(i.SizeOfStackReserve),
		SizeOfStackCommit:           uint64(i.SizeOfStackCommit),
		SizeOfHeapReserve:           uint64(i.SizeOfHeapReserve),
		SizeOfHeapCommit:            uint64(i.SizeOfHeapCommit),
		LoaderFlags:                 i.LoaderFlags,
		NumberOfRvaAndSizes:         i.NumberOfRvaAndSizes,
		DataDirectory:               i.DataDirectory,
	}
}

// ImageNTHeaders32 contains the PE file headers for 32-bit PE images.
type ImageNTHeaders32 struct {
	// Signature identifies the PE format; Always "PE\0\0".
	Signature      [4]byte
	FileHeader     ImageFileHeader
	OptionalHeader ImageOptionalHeader32
}

// ImageNTHeaders64 contains the PE file headers for 64-bit PE images.
type ImageNTHeaders64 struct {
	// Signature identifies the PE format; Always "PE\0\0".
	Signature      [4]byte
	FileHeader     ImageFileHeader
	OptionalHeader ImageOptionalHeader64
}

// To64 converts the ImageNTHeaders32 to an ImageNTHeaders64.
func (i ImageNTHeaders32) To64() ImageNTHeaders64 {
	return ImageNTHeaders64{
		Signature:      i.Signature,
		FileHeader:     i.FileHeader,
		OptionalHeader: i.OptionalHeader.To64(),
	}
}

// ImageDataDirectory holds a record for the given data directory. Each data
// directory contains information about another section, such as the import
// table. The index of the directory entry determines which section it
// pertains to.
type ImageDataDirectory struct {
	VirtualAddress uint32
	Size           uint32
}

// ImageSectionHeader is the header for a section. Windows Loader uses these
// entries to configure the memory mapping of the executable. A series of
// these structures immediately follow the headers.
type ImageSectionHeader struct {
	Name                         [SectionNameLength]byte
	PhysicalAddressOrVirtualSize uint32
	VirtualAddress               uint32
	SizeOfRawData                uint32
	PointerToRawData             uint32
	PointerToRelocations         uint32
	PointerToLinenumbers         uint32
	NumberOfRelocations          uint16
	NumberOfLinenumbers          uint16
	Characteristics              uint32
}

// ImageBaseRelocation holds the header for a single page of base relocation
// data. The .reloc section of the binary contains a series of blocks of
// base relocation data, each starting with this header and followed by n
// 16-bit values that each represent a single relocation. The 4 most
// significant bits specify the type of relocation, while the 12 least
// significant bits contain the lower bits of the address (which is combined
// with the virtual address of the page.) The SizeOfBlock value specifies the
// size of an entire block, in bytes, including its header.
type ImageBaseRelocation struct {
	VirtualAddress uint32
	SizeOfBlock    uint32
}

// The ImageImportDescriptor contains information about an imported module.
type ImageImportDescriptor struct {
	OriginalFirstThunk uint32
	TimeDateStamp      uint32
	ForwarderChain     uint32
	Name               uint32
	FirstThunk         uint32
}

// The ImageExportDirectory contains information about the module's exports.
type ImageExportDirectory struct {
	Characteristics       uint32
	TimeDateStamp         uint32
	MajorVersion          uint16
	MinorVersion          uint16
	Name                  uint32
	Base                  uint32
	NumberOfFunctions     uint32
	NumberOfNames         uint32
	AddressOfFunctions    uint32
	AddressOfNames        uint32
	AddressOfNameOrdinals uint32
}

// ImageTLSDirectory32 contains information about the module's thread local
// storage callbacks (in PE32)
type ImageTLSDirectory32 struct {
	StartAddressOfRawData uint32
	EndAddressOfRawData   uint32
	AddressOfIndex        uint32
	AddressOfCallBacks    uint32
	SizeOfZeroFill        uint32
	Characteristics       uint32
}

// ImageTLSDirectory64 contains information about the module's thread local
// storage callbacks (in PE64)
type ImageTLSDirectory64 struct {
	StartAddressOfRawData uint64
	EndAddressOfRawData   uint64
	AddressOfIndex        uint64
	AddressOfCallBacks    uint64
	SizeOfZeroFill        uint32
	Characteristics       uint32
}

// To64 converts the ImageTLSDirectory32 to an ImageTLSDirectory64.
func (i ImageTLSDirectory32) To64() ImageTLSDirectory64 {
	return ImageTLSDirectory64{
		StartAddressOfRawData: uint64(i.StartAddressOfRawData),
		EndAddressOfRawData:   uint64(i.EndAddressOfRawData),
		AddressOfIndex:        uint64(i.AddressOfIndex),
		AddressOfCallBacks:    uint64(i.AddressOfCallBacks),
		SizeOfZeroFill:        i.SizeOfZeroFill,
		Characteristics:       i.Characteristics,
	}
}
