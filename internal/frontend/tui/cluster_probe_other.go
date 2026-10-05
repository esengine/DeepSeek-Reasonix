//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package tui

func clustersEmoji() (clusters, ok bool) { return false, false }
