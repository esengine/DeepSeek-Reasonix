package main

import (
	"runtime"
	"strconv"
	"strings"
)

// devinfo.go collects coarse machine facts attached to crash reports: OS version,
// CPU model, core count, RAM. Nothing here identifies a user or machine.

type deviceInfo struct {
	OSVersion     string `json:"osVersion,omitempty"`
	OSBuild       int    `json:"osBuild,omitempty"`
	OSRevision    int    `json:"osRevision,omitempty"`
	DistroID      string `json:"distroId,omitempty"`
	DistroVersion string `json:"distroVersion,omitempty"`
	KernelVersion string `json:"kernelVersion,omitempty"`
	SessionType   string `json:"sessionType,omitempty"`
	CPU           string `json:"cpu,omitempty"`
	Cores         int    `json:"cores"`
	RAMGB         int    `json:"ramGb,omitempty"`
}

type platformEnvironment struct {
	DistroID      string
	DistroVersion string
	KernelVersion string
	SessionType   string
}

const gib = 1 << 30

func collectDeviceInfo() deviceInfo {
	osBuild, osRevision := platformOSBuild()
	environment := platformEnvironmentInfo()
	return deviceInfo{
		OSVersion:     boundedPlatformField(platformOSVersion(), 128),
		OSBuild:       osBuild,
		OSRevision:    osRevision,
		DistroID:      environment.DistroID,
		DistroVersion: environment.DistroVersion,
		KernelVersion: environment.KernelVersion,
		SessionType:   environment.SessionType,
		CPU:           boundedPlatformField(platformCPU(), 128),
		Cores:         runtime.NumCPU(),
		RAMGB:         int((platformRAMBytes() + gib/2) / gib),
	}
}

func boundedPlatformField(value string, limit int) string {
	value = strings.TrimSpace(value)
	value = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, value)
	if len(value) > limit {
		value = value[:limit]
	}
	return value
}

func parseOSRelease(osRelease string) map[string]string {
	result := make(map[string]string)
	for line := range strings.SplitSeq(osRelease, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
			value = strings.ReplaceAll(value, `\"`, `"`)
			value = strings.ReplaceAll(value, `\\`, `\`)
		}
		result[key] = value
	}
	return result
}

func parseCPUModel(cpuinfo string) string {
	for line := range strings.SplitSeq(cpuinfo, "\n") {
		if name, ok := strings.CutPrefix(line, "model name"); ok {
			if _, v, ok := strings.Cut(name, ":"); ok {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}

func parseMemTotalBytes(meminfo string) uint64 {
	for line := range strings.SplitSeq(meminfo, "\n") {
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
	return parseOSRelease(osRelease)["PRETTY_NAME"]
}

func linuxSessionType(getenv func(string) string) string {
	if getenv("SSH_CONNECTION") != "" || getenv("XRDP_SESSION") != "" {
		return "remote"
	}
	switch strings.ToLower(strings.TrimSpace(getenv("XDG_SESSION_TYPE"))) {
	case "wayland":
		return "wayland"
	case "x11":
		return "x11"
	}
	if getenv("WAYLAND_DISPLAY") != "" {
		return "wayland"
	}
	if getenv("DISPLAY") != "" {
		return "x11"
	}
	return "unknown"
}
