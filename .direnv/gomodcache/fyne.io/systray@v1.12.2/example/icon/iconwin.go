//go:build windows

package icon

import (
    _ "embed"
)

//go:embed "iconwin.ico"
var Data []byte
