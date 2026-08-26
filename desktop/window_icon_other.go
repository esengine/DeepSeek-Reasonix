//go:build !windows

package main

// applyWindowIconsFromExecutable is a Windows-only fix: Wails v2 creates its
// window class without an hIcon, and once the process sets an explicit
// AppUserModelID (PR #7925), Win11's taskbar prefers AUMID-registered
// resources over the executable's embedded icon, so the running window's
// taskbar button renders the generic blank-document glyph. The Windows
// implementation loads the executable's own icon and assigns it to the
// window via WM_SETICON. No-op elsewhere.
func applyWindowIconsFromExecutable() {}
