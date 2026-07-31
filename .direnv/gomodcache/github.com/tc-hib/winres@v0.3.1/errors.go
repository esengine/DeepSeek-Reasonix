package winres

import "errors"

const (
	errZeroID          = "ordinal identifier must not be zero"
	errEmptyName       = "string identifier must not be empty"
	errNameContainsNUL = "string identifier must not contain NUL char"

	errUnknownArch = "unknown architecture"

	errNotICO                 = "not a valid ICO file"
	errImageLengthTooBig      = "image size found in ICONDIRENTRY is too big (above 10 MB)"
	errTooManyIconSizes       = "too many sizes"
	errGroupNotFound          = "group does not exist"
	errInvalidGroup           = "invalid group"
	errIconMissing            = "icon missing from group"
	errCursorMissing          = "cursor missing from group"
	errInvalidImageDimensions = "invalid image dimensions"
	errImageTooBig            = "image size too big, must fit in 256x256"
	errNotCUR                 = "not a valid CUR file"
	errUnknownImageFormat     = "unknown image format"

	errInvalidResDir        = "invalid resource directory"
	errDataEntryOutOfBounds = "data entry out of bounds"

	errNotPEImage    = "not a valid PE image"
	errSignedPE      = "cannot modify a signed PE image"
	errUnknownPE     = "unknown PE format"
	errNoRSRC        = "image doesn't have a resource directory" // This is when the data directory entry is zero
	errRSRCNotFound  = "resource section not found"              // This is when the data directory entry is not zero
	errSectionTooFar = "invalid section header points too far"
	errNoRoomForRSRC = "not enough room to add .rsrc section header"
	errRSRCTwice     = "found resource section twice"
	errRelocTwice    = "found reloc section twice"

	errInvalidVersion      = "invalid version number"
	errUnknownSupportedOS  = "unknown minimum-os value"
	errUnknownDPIAwareness = "unknown dpi-awareness value"
	errUnknownExecLevel    = "unknown execution-level value"
)

// ErrNoResources is the error returned by LoadFromEXE when it didn't find a .rsrc section.
var ErrNoResources = errors.New(errNoRSRC)

// ErrSignedPE is the error returned by WriteToEXE when it refused to touch signed code. (Authenticode)
var ErrSignedPE = errors.New(errSignedPE)
