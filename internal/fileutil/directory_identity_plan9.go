//go:build plan9

package fileutil

import "fmt"

func DirectoryIdentity(string) (string, error) {
	return "", fmt.Errorf("directory publication identity unsupported")
}
