package main

import (
	"os"
	"strings"
)

func readOr(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

func platformOSVersion() string {
	if name := parseOSReleasePrettyName(readOr("/etc/os-release")); name != "" {
		return name
	}
	return "Linux"
}

func platformOSBuild() (int, int) { return 0, 0 }

func platformEnvironmentInfo() platformEnvironment {
	release := parseOSRelease(readOr("/etc/os-release"))
	distroID := boundedPlatformField(strings.ToLower(release["ID"]), 64)
	if distroID == "" {
		distroID = "unknown"
	}
	return platformEnvironment{
		DistroID:      distroID,
		DistroVersion: boundedPlatformField(release["VERSION_ID"], 64),
		KernelVersion: boundedPlatformField(readOr("/proc/sys/kernel/osrelease"), 128),
		SessionType:   linuxSessionType(os.Getenv),
	}
}

func platformCPU() string {
	return parseCPUModel(readOr("/proc/cpuinfo"))
}

func platformRAMBytes() uint64 {
	return parseMemTotalBytes(readOr("/proc/meminfo"))
}
