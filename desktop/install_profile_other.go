//go:build !windows

package main

func telemetryInstallProfile() string {
	return metricBucket(detectInstallProfile().Mode)
}
