//go:build windows

package tui

import (
	"sync"
	"unicode"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	defaultCharset       = 1
	glyphMissingMark     = 1 // GGI_MARK_NONEXISTING_GLYPHS
	glyphMissing         = 0xffff
	conhostWindowClass   = "ConsoleWindowClass"
	consoleFaceNameChars = 32
)

var (
	user32DLL                   = windows.NewLazySystemDLL("user32.dll")
	gdi32DLL                    = windows.NewLazySystemDLL("gdi32.dll")
	procGetConsoleWindow        = kernel32DLL.NewProc("GetConsoleWindow")
	procGetCurrentConsoleFontEx = kernel32DLL.NewProc("GetCurrentConsoleFontEx")
	procGetClassNameW           = user32DLL.NewProc("GetClassNameW")
	procCreateCompatibleDC      = gdi32DLL.NewProc("CreateCompatibleDC")
	procCreateFontIndirectW     = gdi32DLL.NewProc("CreateFontIndirectW")
	procSelectObject            = gdi32DLL.NewProc("SelectObject")
	procGetGlyphIndicesW        = gdi32DLL.NewProc("GetGlyphIndicesW")
	procGetCharWidth32W         = gdi32DLL.NewProc("GetCharWidth32W")
)

type consoleFontInfoEx struct {
	size     uint32
	font     uint32
	fontSize windows.Coord
	family   uint32
	weight   uint32
	face     [consoleFaceNameChars]uint16
}

type logFont struct {
	height, width, escapement, orientation, weight int32
	italic, underline, strikeOut, charSet          byte
	outPrecision, clipPrecision, quality, pitch    byte
	face                                           [consoleFaceNameChars]uint16
}

// consoleFontGlyphs reports whether the font a conhost window draws with has
// a glyph for a rune no wider than two cells, or nil when the console is a
// pseudoconsole whose terminal picks fonts itself. Han, kana, hangul and
// fullwidth forms reach conhost through the font links Windows configures.
func consoleFontGlyphs() func(rune) bool {
	dc := consoleFontDC()
	if dc == 0 {
		return nil
	}
	var mu sync.Mutex
	advance := func(r rune) int32 {
		var w int32
		if ok, _, _ := procGetCharWidth32W.Call(dc, uintptr(r), uintptr(r), uintptr(unsafe.Pointer(&w))); ok == 0 {
			return -1
		}
		return w
	}
	mu.Lock()
	cell := advance('0')
	mu.Unlock()
	if cell <= 0 {
		return nil
	}
	return func(r rune) bool {
		if r < 0x80 || linkedScript(r) {
			return true
		}
		u := utf16.Encode([]rune{r})
		if len(u) != 1 {
			return false
		}
		var idx uint16
		mu.Lock()
		defer mu.Unlock()
		got, _, _ := procGetGlyphIndicesW.Call(dc, uintptr(unsafe.Pointer(&u[0])), 1, uintptr(unsafe.Pointer(&idx)), glyphMissingMark)
		if got != 1 || idx == glyphMissing {
			return false
		}
		w := advance(r)
		return w >= 0 && w <= 2*cell
	}
}

func linkedScript(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul, unicode.Bopomofo) ||
		r >= 0x3000 && r <= 0x303f || r >= 0xff00 && r <= 0xffef
}

// consoleFontDC is a memory device context holding the console window's
// current font, or 0 when the console is not a conhost window.
func consoleFontDC() uintptr {
	hwnd, _, _ := procGetConsoleWindow.Call()
	if hwnd == 0 {
		return 0
	}
	var class [64]uint16
	n, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&class[0])), uintptr(len(class)))
	if n == 0 || windows.UTF16ToString(class[:n]) != conhostWindowClass {
		return 0
	}
	out, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	if err != nil {
		return 0
	}
	info := consoleFontInfoEx{size: uint32(unsafe.Sizeof(consoleFontInfoEx{}))}
	if ok, _, _ := procGetCurrentConsoleFontEx.Call(uintptr(out), 0, uintptr(unsafe.Pointer(&info))); ok == 0 {
		return 0
	}
	lf := logFont{height: -int32(info.fontSize.Y), weight: int32(info.weight), charSet: defaultCharset, face: info.face}
	font, _, _ := procCreateFontIndirectW.Call(uintptr(unsafe.Pointer(&lf)))
	dc, _, _ := procCreateCompatibleDC.Call(0)
	if font == 0 || dc == 0 {
		return 0
	}
	procSelectObject.Call(dc, font)
	return dc
}
