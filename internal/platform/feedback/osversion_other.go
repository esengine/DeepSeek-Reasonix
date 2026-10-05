//go:build !windows && !darwin

package feedback

import (
	"os"
	"strings"
)

func osVersion() string {
	b, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
