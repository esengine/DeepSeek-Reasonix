//go:build linux && cgo && reasonix_transcript_smoke

package main

/*
#cgo CFLAGS: -DREASONIX_TRANSCRIPT_SMOKE
#cgo !webkit2_41 pkg-config: gtk+-3.0 webkit2gtk-4.0
#cgo webkit2_41 pkg-config: gtk+-3.0 webkit2gtk-4.1
#include <stdlib.h>
char *reasonix_transcript_smoke_linux(const char *url, const char *script);
*/
import "C"

import (
	"errors"
	"unsafe"
)

func runTranscriptNativeSmoke(url, script string) (string, error) {
	cURL := C.CString(url)
	cScript := C.CString(script)
	defer C.free(unsafe.Pointer(cURL))
	defer C.free(unsafe.Pointer(cScript))
	result := C.reasonix_transcript_smoke_linux(cURL, cScript)
	if result == nil {
		return "", errors.New("WebKitGTK host returned no result")
	}
	defer C.free(unsafe.Pointer(result))
	return C.GoString(result), nil
}
