package proc

import "errors"

// ErrProcessTrackingUnavailable means a caller that requires an OS-enforced
// process-tree lifetime could not establish it before the child began running.
var ErrProcessTrackingUnavailable = errors.New("process tree tracking is unavailable")
