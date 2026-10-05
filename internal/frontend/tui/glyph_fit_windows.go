//go:build windows

package tui

import (
	"os"
	"sync"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	consoleTextmodeBuffer = 1
	codePageUSASCII       = 20127
)

var (
	kernel32DLL                   = windows.NewLazySystemDLL("kernel32.dll")
	procCreateConsoleScreenBuffer = kernel32DLL.NewProc("CreateConsoleScreenBuffer")
	procWideCharToMultiByte       = kernel32DLL.NewProc("WideCharToMultiByte")
)

// measureBuffer is a never-activated screen buffer of the process's console:
// it shares the window's font, so writing a rune into it and reading the cursor
// back gives the width the console draws without touching the visible screen.
var measureBuffer = sync.OnceValue(func() windows.Handle {
	h, _, _ := procCreateConsoleScreenBuffer.Call(
		uintptr(windows.GENERIC_READ|windows.GENERIC_WRITE),
		uintptr(windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE),
		0, consoleTextmodeBuffer, 0)
	if h == 0 || windows.Handle(h) == windows.InvalidHandle {
		return windows.InvalidHandle
	}
	return windows.Handle(h)
})

// newConsoleGlyphFit returns nil unless out is a console the process can
// measure. A pseudoconsole measures ambiguous-width runes as one cell and its
// terminal picks its own fonts, so there only what no font draws is replaced.
func newConsoleGlyphFit(out *os.File) *glyphFit {
	var mode uint32
	if windows.GetConsoleMode(windows.Handle(out.Fd()), &mode) != nil {
		return nil
	}
	h := measureBuffer()
	if h == windows.InvalidHandle {
		return nil
	}
	return newGlyphFit(func(r rune) int { return consoleCells(h, r) }, asciiBestFit, consoleFontGlyphs())
}

func consoleCells(h windows.Handle, r rune) int {
	if windows.SetConsoleCursorPosition(h, windows.Coord{}) != nil {
		return 0
	}
	u := utf16.Encode([]rune{r})
	var written uint32
	if windows.WriteConsole(h, &u[0], uint32(len(u)), &written, nil) != nil {
		return 0
	}
	var info windows.ConsoleScreenBufferInfo
	if windows.GetConsoleScreenBufferInfo(h, &info) != nil || info.CursorPosition.Y != 0 {
		return 0
	}
	return int(info.CursorPosition.X)
}

// asciiBestFit asks the OS for its US-ASCII best-fit of r; a rune it has no
// mapping for comes back as the code page's default character, which is none.
func asciiBestFit(r rune) rune {
	u := utf16.Encode([]rune{r})
	var out [2]byte
	var defaulted int32
	n, _, _ := procWideCharToMultiByte.Call(codePageUSASCII, 0,
		uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)),
		uintptr(unsafe.Pointer(&out[0])), uintptr(len(out)), 0, uintptr(unsafe.Pointer(&defaulted)))
	if n != 1 || defaulted != 0 || out[0] < 0x20 || out[0] > 0x7e {
		return 0
	}
	return rune(out[0])
}
