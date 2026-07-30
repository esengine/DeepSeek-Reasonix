//go:build windows

package repair

import (
	"encoding/binary"

	"golang.org/x/sys/windows"
)

func platformRepairPathCaseInsensitive(path string) bool {
	parent := existingRepairPathParent(path)
	if parent == "" {
		return true
	}
	name, err := windows.UTF16PtrFromString(parent)
	if err != nil {
		return true
	}
	handle, err := windows.CreateFile(
		name,
		windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return true
	}
	defer windows.CloseHandle(handle)

	var info [4]byte
	err = windows.GetFileInformationByHandleEx(
		handle,
		windows.FileCaseSensitiveInfo,
		&info[0],
		uint32(len(info)),
	)
	if err == nil {
		return binary.LittleEndian.Uint32(info[:])&windows.FILE_CS_FLAG_CASE_SENSITIVE_DIR == 0
	}
	// Per-directory case sensitivity was added after the API itself. Systems
	// that cannot answer the query retain legacy case-insensitive lookup.
	return true
}
