//go:build windows

package main

import (
	"fmt"
	"io"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

var readVerifiedWindowsStagedPayloadFn = readVerifiedWindowsStagedPayload

// readVerifiedWindowsStagedPayload verifies the Authenticode signature and
// reads the payload through one handle that disallows concurrent writes,
// deletes, and renames. The bytes handed to the publisher therefore cannot
// change between signature verification and publication.
func readVerifiedWindowsStagedPayload(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("staged payload is not a regular file")
	}
	path16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(
		path16,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_SEQUENTIAL_SCAN,
		0,
	)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), path)
	if file == nil {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("open staged payload handle")
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !fileInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("staged payload is not a regular file")
	}

	trustFile := &windows.WinTrustFileInfo{
		Size:     uint32(unsafe.Sizeof(windows.WinTrustFileInfo{})),
		FilePath: path16,
		File:     handle,
	}
	trustData := &windows.WinTrustData{
		Size:                            uint32(unsafe.Sizeof(windows.WinTrustData{})),
		UIChoice:                        windows.WTD_UI_NONE,
		RevocationChecks:                windows.WTD_REVOKE_NONE,
		UnionChoice:                     windows.WTD_CHOICE_FILE,
		FileOrCatalogOrBlobOrSgnrOrCert: unsafe.Pointer(trustFile),
		StateAction:                     windows.WTD_STATEACTION_VERIFY,
		UIContext:                       windows.WTD_UICONTEXT_EXECUTE,
	}
	verifyErr := windows.WinVerifyTrustEx(
		windows.InvalidHWND,
		&windows.WINTRUST_ACTION_GENERIC_VERIFY_V2,
		trustData,
	)
	trustData.StateAction = windows.WTD_STATEACTION_CLOSE
	closeErr := windows.WinVerifyTrustEx(
		windows.InvalidHWND,
		&windows.WINTRUST_ACTION_GENERIC_VERIFY_V2,
		trustData,
	)
	if verifyErr != nil {
		if closeErr != nil {
			return nil, fmt.Errorf("Authenticode verification failed: %w (release trust state: %w)", verifyErr, closeErr)
		}
		return nil, fmt.Errorf("Authenticode verification failed: %w", verifyErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("release Authenticode verification state: %w", closeErr)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return io.ReadAll(file)
}
