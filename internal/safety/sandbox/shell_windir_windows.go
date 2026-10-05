package sandbox

import "golang.org/x/sys/windows"

func systemWindowsDir() string {
	dir, err := windows.GetSystemWindowsDirectory()
	if err != nil {
		return ""
	}
	return dir
}
