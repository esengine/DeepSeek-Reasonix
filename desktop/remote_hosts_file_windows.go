//go:build windows

package main

import "os"

func openRemoteHostStoreFile(path string) (*os.File, os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, ErrRemoteHostStoreUnsafe
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	openedInfo, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, nil, err
	}
	return file, openedInfo, nil
}

// Windows protects the file using the current user's ACL. os.FileMode does not
// represent that ACL, while AtomicWriteFile still creates the file privately.
func validateRemoteHostStorePermissions(os.FileInfo) error { return nil }
