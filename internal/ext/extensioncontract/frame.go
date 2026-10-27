package extensioncontract

import "regexp"

// MaxFramesPerPackage bounds how many frame documents one package may declare.
const MaxFramesPerPackage = 8

var frameIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// ValidFrameID reports whether id is a frame id a manifest may declare and a
// frame surface may name.
func ValidFrameID(id string) bool { return frameIDPattern.MatchString(id) }
