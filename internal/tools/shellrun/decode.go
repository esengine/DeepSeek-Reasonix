package shellrun

import (
	fileenc "reasonix/internal/base/fileutil/encoding"
)

// decodeShellOutput reads a child's bytes as text. A Windows console tool
// answers in the machine's code page rather than UTF-8, and the bytes were kept
// as a Go string unchanged: "FIND: 参数格式不正确" reached the model as U+FFFD once
// JSON coerced them, so four failed calls never said why they failed.
// cut says which ends of b a buffer bound dropped bytes from.
func decodeShellOutput(b []byte, cut fileenc.Cut) string {
	return string(fileenc.DecodeOutput(b, cut))
}
