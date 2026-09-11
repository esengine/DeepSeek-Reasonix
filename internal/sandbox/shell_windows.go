//go:build windows

package sandbox

import (
	"golang.org/x/sys/windows/registry"
)

func windowsRegistryGitRoots() []string {
	var roots []string
	for _, rootKey := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
		for _, subKey := range []string{
			`SOFTWARE\GitForWindows`,
			`SOFTWARE\WOW6432Node\GitForWindows`,
			`SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\Git_is1`,
		} {
			k, err := registry.OpenKey(rootKey, subKey, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			for _, valName := range []string{"InstallPath", "InstallLocation"} {
				if val, _, err := k.GetStringValue(valName); err == nil && val != "" {
					roots = append(roots, val)
				}
			}
			_ = k.Close()
		}
	}
	return roots
}
