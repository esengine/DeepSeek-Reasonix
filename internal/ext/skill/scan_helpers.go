package skill

import (
	"os"
	"strings"
)

func shouldStatEntryTarget(mode os.FileMode) bool {
	return mode&os.ModeSymlink != 0 || mode&os.ModeIrregular != 0
}

func shouldSkipScanDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch strings.ToLower(name) {
	case "assets", "node_modules", "references", "scripts":
		return true
	default:
		return false
	}
}
