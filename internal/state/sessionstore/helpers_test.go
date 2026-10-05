package sessionstore

import "os"

func writeFile(path string, b []byte) error { return os.WriteFile(path, b, 0o644) }
