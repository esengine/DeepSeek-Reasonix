// Copyright (c) 2019, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

//go:build windows

package interp_test

import "golang.org/x/sys/windows"

// shortPathName is used for testing against DOS short names.
//
// Only used for testing, so we assume that a short path always fits in
// 2*len(path) in UTF-16.
func shortPathName(path string) (string, error) {
	src, err := windows.UTF16FromString(path)
	if err != nil {
		return "", err
	}
	dst := make([]uint16, len(src)*2)
	if _, err := windows.GetShortPathName(&src[0], &dst[0], uint32(len(dst))); err != nil {
		return "", err
	}
	return windows.UTF16ToString(dst), nil
}
