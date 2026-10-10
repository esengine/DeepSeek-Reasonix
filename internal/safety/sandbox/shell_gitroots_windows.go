package sandbox

import "golang.org/x/sys/windows/registry"

// gitInstallRoots lists the InstallPath values Git for Windows writes for a
// machine-wide and a per-user install, in both registry views.
func gitInstallRoots() []string {
	return readGitInstallRoots(func(root registry.Key, view uint32) string {
		k, err := registry.OpenKey(root, `SOFTWARE\GitForWindows`, registry.QUERY_VALUE|view)
		if err != nil {
			return ""
		}
		defer k.Close()
		v, _, err := k.GetStringValue("InstallPath")
		if err != nil {
			return ""
		}
		return v
	})
}

func readGitInstallRoots(read func(root registry.Key, view uint32) string) []string {
	var out []string
	for _, root := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
		for _, view := range []uint32{registry.WOW64_64KEY, registry.WOW64_32KEY} {
			if v := read(root, view); v != "" {
				out = append(out, v)
			}
		}
	}
	return out
}
