package provider

import "errors"

// ErrEffortRefused marks a construction refused because the endpoint has no
// such reasoning effort; a caller may retry with another effort, never with
// the same config.
var ErrEffortRefused = errors.New("effort not supported by this endpoint")
