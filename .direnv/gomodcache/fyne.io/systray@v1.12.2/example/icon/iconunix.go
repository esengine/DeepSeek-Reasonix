//go:build linux || darwin

package icon

import (
    _ "embed"
)

//go:embed icon.png
var Data []byte
