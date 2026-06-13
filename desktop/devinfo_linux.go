package main

import "os"

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

func platformCPU() string {
	return parseCPUModel(readOr("/proc/cpuinfo"))
}

func platformRAMBytes() uint64 {
	return parseMemTotalBytes(readOr("/proc/meminfo"))
}
