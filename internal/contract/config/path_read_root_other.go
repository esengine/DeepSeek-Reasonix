//go:build !windows

package config

func configReadRootPlatformUnavailable(error) bool { return false }
