package main

import (
	"os/exec"
	"strconv"
	"strings"
)

func sysctlString(name string) string {
	out, err := exec.Command("sysctl", "-n", name).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func platformOSVersion() string {
	out, err := exec.Command("sw_vers", "-productVersion").Output()
	if err != nil {
		return "macOS"
	}
	return "macOS " + strings.TrimSpace(string(out))
}

func platformCPU() string {
	return sysctlString("machdep.cpu.brand_string")
}

func platformRAMBytes() uint64 {
	n, err := strconv.ParseUint(sysctlString("hw.memsize"), 10, 64)
	if err != nil {
		return 0
	}
	return n
}
