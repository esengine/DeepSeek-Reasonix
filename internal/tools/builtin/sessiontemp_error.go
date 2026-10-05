package builtin

import "errors"

// errSessionTemp marks the step an error came from, so a caller reads the
// failure phase off the error's identity rather than off the prefix it happens
// to carry. bash and grep both wrap with it; neither owns it.
var errSessionTemp = errors.New("session temporary directory")
