//go:build !unix

package attachment

import "os"

const imageReadFlags = os.O_RDONLY
