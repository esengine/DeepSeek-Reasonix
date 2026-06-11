package main

import (
	"runtime"
	"strconv"
	"strings"
)

// devinfo.go collects coarse machine facts attached to crash reports: OS version,
// CPU model, core count, RAM. Nothing here identifies a user or machine.

type deviceInfo struct {
	OSVersion string `json:"osVersion,omitempty"`
	CPU       string `json:"cpu,omitempty"`
	Cores     int    `json:"cores"`
	RAMGB     int    `json:"ramGb,omitempty"`
}

const gib = 1 << 30

func collectDeviceInfo() deviceInfo {
	return deviceInfo{
		OSVersion: platformOSVersion(),
		CPU:       platformCPU(),
		Cores:     runtime.NumCPU(),
		RAMGB:     int((platformRAMBytes() + gib/2) / gib),
	}
}

func parseCPUModel(cpuinfo string) string {
	for _, line := range strings.Split(cpuinfo, "\n") {
		if name, ok := strings.CutPrefix(line, "model name"); ok {
			if _, v, ok := strings.Cut(name, ":"); ok {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}

func parseMemTotalBytes(meminfo string) uint64 {
	for _, line := range strings.Split(meminfo, "\n") {
		if rest, ok := strings.CutPrefix(line, "MemTotal:"); ok {
			kb, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimSpace(rest), " kB"), 10, 64)
			if err != nil {
				return 0
			}
			return kb * 1024
		}
	}
	return 0
}

func parseOSReleasePrettyName(osRelease string) string {
	for _, line := range strings.Split(osRelease, "\n") {
		if v, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
			return strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	return ""
}
