package provider

import "sync/atomic"

// UploadObserver is told how many request-body bytes each send attempt puts on
// the wire, labelled with the provider instance that sent it.
type UploadObserver func(provider string, bytes int64)

var uploadObserver atomic.Pointer[UploadObserver]

// SetUploadObserver installs the one process-wide observer; nil removes it.
func SetUploadObserver(fn UploadObserver) {
	if fn == nil {
		uploadObserver.Store(nil)
		return
	}
	uploadObserver.Store(&fn)
}

// noteUpload reports a body of known length; an unknown length (-1) or no body
// reports nothing rather than a guess.
func noteUpload(provider string, contentLength int64) {
	if contentLength <= 0 {
		return
	}
	if fn := uploadObserver.Load(); fn != nil {
		(*fn)(provider, contentLength)
	}
}
