//go:build darwin && cgo && reasonix_transcript_smoke

package main

/*
#cgo CFLAGS: -DREASONIX_TRANSCRIPT_SMOKE -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework WebKit -framework CoreGraphics
#include <stdlib.h>
char *reasonix_transcript_smoke_darwin(const char *url, const char *script);
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
	result := C.reasonix_transcript_smoke_darwin(cURL, cScript)
	if result == nil {
		return "", errors.New("WKWebView host returned no result")
	}
	defer C.free(unsafe.Pointer(result))
	return C.GoString(result), nil
}
