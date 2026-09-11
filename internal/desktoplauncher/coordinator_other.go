//go:build !windows

package desktoplauncher

func coordinatedLaunch(root string, args []string) (bool, int) { return false, 0 }
