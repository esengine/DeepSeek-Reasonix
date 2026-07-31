// Package version provides functions to build a VERSIONINFO structure for Windows applications.
//
// This what Windows displays in the Details tab of file properties.
//
package version

import (
	"errors"
	"io"
	"strings"
	"time"
)

const (
	Comments         = "Comments"
	CompanyName      = "CompanyName"
	FileDescription  = "FileDescription"
	FileVersion      = "FileVersion"
	InternalName     = "InternalName"
	LegalCopyright   = "LegalCopyright"
	LegalTrademarks  = "LegalTrademarks"
	OriginalFilename = "OriginalFilename"
	PrivateBuild     = "PrivateBuild"
	ProductName      = "ProductName"
	ProductVersion   = "ProductVersion"
	SpecialBuild     = "SpecialBuild"
)

type fileType int

const (
	App fileType = iota
	DLL
	Unknown
)

const (
	// LangNeutral is the LCID for language agnostic data.
	LangNeutral = 0
	// LangDefault is the LCID for en-US, and it is the default in many tools and APIs.
	LangDefault = 0x409
)

// Info is the main struct of this package.
// Create one as Info{}, use Info.Set() to add key/value pairs,
// set other members, then add it to the resource set with Info.AddTo.
type Info struct {
	FileVersion    [4]uint16
	ProductVersion [4]uint16
	Flags          versionFlags
	Type           fileType
	Timestamp      time.Time
	lt             LangTable

	// temporary state
	pos int
	w   io.Writer
}

type versionFlags struct {
	Debug        bool
	Prerelease   bool
	Patched      bool
	PrivateBuild bool
	SpecialBuild bool
}

// LangTable holds different languages.
type LangTable map[uint16]*StringTable

// StringTable holds key/value metadata.
type StringTable map[string]string

// Table returns a copy of the language and containing string tables,
// which hold key/value metadata.
func (vi *Info) Table() LangTable {
	if vi == nil || vi.lt == nil {
		return nil
	}

	lt := make(LangTable, len(vi.lt))
	for lang, langST := range vi.lt {
		var st *StringTable
		if langST != nil {
			stm := make(StringTable, len(*langST))
			for k, v := range *langST {
				stm[k] = v
			}
			st = &stm
		}
		lt[lang] = st
	}

	return lt
}

// Set sets a key/value pair in the Info structure for a specific locale.
//
// Standard keys are defined as constants in this package: version.ProductName, version.CompanyName, ...
//
// Strings must not contain NUL characters.
//
// Language Code Identifiers (LCID) are listed there: https://docs.microsoft.com/en-us/openspecs/windows_protocols/ms-lcid/
//
// langID may also be 0 for neutral.
func (vi *Info) Set(langID uint16, key string, value string) error {
	if vi.lt == nil {
		vi.lt = make(LangTable)
	}
	if key == "" {
		return errors.New(errEmptyKey)
	}
	if strings.ContainsRune(key, 0) {
		return errors.New(errKeyContainsNUL)
	}
	if strings.ContainsRune(value, 0) {
		return errors.New(errValueContainsNUL)
	}
	st, ok := vi.lt[langID]
	if !ok {
		st = &StringTable{}
		vi.lt[langID] = st
	}
	(*st)[key] = value
	return nil
}

// Bytes returns the binary representation of the VS_VERSIONINFO struct.
func (vi *Info) Bytes() []byte {
	if vi == nil {
		return nil
	}
	return vi.bytes()
}

// FromBytes loads an Info from the binary representation of a VS_VERSIONINFO struct.
func FromBytes(data []byte) (*Info, error) {
	return fromBytes(data)
}

// SplitTranslations splits the Info struct by language.
// It returns a map indexed by language ID.
//
// Windows Explorer doesn't seem to search for a proper translation inside the VERSIONINFO struct.
// So one has to embed a whole VERSIONINFO for each language as a resource translation.
func (vi *Info) SplitTranslations() map[uint16]*Info {
	if vi == nil {
		return nil
	}
	return vi.splitLangs()
}

// SetProductVersion sets the product version, ensuring this is the only one in the structure.
//
// This should be called after json.Unmarshal to override the version.
func (vi *Info) SetProductVersion(productVersion string) {
	vi.ProductVersion = versionStringToArray(productVersion)
	if len(vi.lt) == 0 {
		vi.Set(LangNeutral, ProductVersion, productVersion)
		return
	}
	for langID := range vi.lt {
		vi.Set(langID, ProductVersion, productVersion)
	}
}

// SetFileVersion sets the file version, ensuring this is the only one in the structure.
//
// This should be called after json.Unmarshal to override the version.
func (vi *Info) SetFileVersion(fileVersion string) {
	vi.FileVersion = versionStringToArray(fileVersion)
	if len(vi.lt) == 0 {
		vi.Set(LangNeutral, FileVersion, fileVersion)
		return
	}
	for langID := range vi.lt {
		vi.Set(langID, FileVersion, fileVersion)
	}
}

// MergeTranslations merges several VERSIONINFO structs into one multilingual struct.
//
// The fixed part will be taken in priority from:
//  1. The neutral language (zero)
//  2. The default language (en-US)
//  3. The first language ID in ascending order
//
// Each struct corresponds to one translation, and its language ID will be the one it is mapped to.
//
// This means that each struct is supposed to contain exactly one translation,
// either neutral or of same language ID as it is mapped to.
//
// If a struct contains several translations, those that don't correspond to the map key will be ignored.
//
// If a struct contains one translation with a different language ID,
// it will be imported as if it had been the same value as the map key.
//
func MergeTranslations(translations map[uint16]*Info) *Info {
	vi := &Info{}

	main := findMainTranslation(translations)
	if main == nil {
		return vi
	}
	vi.Flags = main.Flags
	vi.Type = main.Type
	vi.Timestamp = main.Timestamp
	vi.ProductVersion = main.ProductVersion
	vi.FileVersion = main.FileVersion

	for langID, trans := range translations {
		st := trans.lt[langID]
		if st == nil || len(*st) == 0 {
			st = trans.lt[LangNeutral]
			if st == nil || len(*st) == 0 {
				st = trans.singleLang()
				if st == nil {
					continue
				}
			}
		}
		for k, v := range *st {
			vi.Set(langID, k, v)
		}
	}

	return vi
}

func (vi *Info) singleLang() *StringTable {
	var (
		seen bool
		lang *StringTable
	)

	for _, st := range vi.lt {
		if st != nil && len(*st) != 0 {
			if seen {
				return nil
			}
			seen = true
			lang = st
		}
	}

	return lang
}

func findMainTranslation(translations map[uint16]*Info) *Info {
	main := translations[LangNeutral]
	if main != nil {
		return main
	}

	main = translations[LangDefault]
	if main != nil {
		return main
	}

	var lang uint16 = 0xFFFF
	for k := range translations {
		if k < lang {
			lang = k
		}
	}
	return translations[lang]
}

func versionStringToArray(v string) [4]uint16 {
	var (
		part int
		ver  [4]uint16
		i    int
	)

	for i = range v {
		if '0' <= v[i] && v[i] <= '9' {
			break
		}
	}
	v = v[i:]
	for i = range v {
		if '0' <= v[i] && v[i] <= '9' {
			ver[part] = ver[part]*10 + uint16(v[i]-'0')
		} else if v[i] == '.' && part < 3 {
			part++
		} else {
			break
		}
	}
	return ver
}

func (vi *Info) splitLangs() map[uint16]*Info {
	m := make(map[uint16]*Info)

	defaults := map[string]string{}
	if st := vi.lt[LangNeutral]; st != nil {
		for k, v := range *st {
			if _, ok := defaults[k]; !ok {
				defaults[k] = v
			}
		}
	}
	if st := vi.lt[LangDefault]; st != nil {
		for k, v := range *st {
			if _, ok := defaults[k]; !ok {
				defaults[k] = v
			}
		}
	}
	for _, langID := range vi.lt.sortedKeys() {
		for k, v := range *vi.lt[langID] {
			if _, ok := defaults[k]; !ok {
				defaults[k] = v
			}
		}
	}

	for langID, st := range vi.lt {
		trans := &Info{
			FileVersion:    vi.FileVersion,
			ProductVersion: vi.ProductVersion,
			Flags:          vi.Flags,
			Type:           vi.Type,
			Timestamp:      vi.Timestamp,
		}
		for k, v := range defaults {
			trans.Set(langID, k, v)
		}
		for k, v := range *st {
			trans.Set(langID, k, v)
		}
		m[langID] = trans
	}

	return m
}

// GetMainTranslation returns the main translation.
func (lt LangTable) GetMainTranslation() StringTable {
	main := lt[LangNeutral]
	if main != nil {
		return *main
	}

	main = lt[LangDefault]
	if main != nil {
		return *main
	}

	var lang uint16 = 0xFFFF
	for k := range lt {
		if k < lang {
			lang = k
		}
	}
	return *lt[lang]
}
