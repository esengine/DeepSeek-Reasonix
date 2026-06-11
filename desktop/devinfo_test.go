package main

import "testing"

func TestParseCPUModel(t *testing.T) {
	cpuinfo := "processor\t: 0\nvendor_id\t: GenuineIntel\nmodel name\t: Intel(R) Core(TM) i7-12700K\nflags\t: fpu vme\n"
	if got := parseCPUModel(cpuinfo); got != "Intel(R) Core(TM) i7-12700K" {
		t.Errorf("parseCPUModel = %q", got)
	}
	if got := parseCPUModel("no such field"); got != "" {
		t.Errorf("parseCPUModel on garbage = %q, want empty", got)
	}
}

func TestParseMemTotalBytes(t *testing.T) {
	meminfo := "MemTotal:       32652284 kB\nMemFree:         1234 kB\n"
	if got := parseMemTotalBytes(meminfo); got != 32652284*1024 {
		t.Errorf("parseMemTotalBytes = %d", got)
	}
	if got := parseMemTotalBytes("MemFree: 1 kB"); got != 0 {
		t.Errorf("parseMemTotalBytes without MemTotal = %d, want 0", got)
	}
}

func TestParseOSReleasePrettyName(t *testing.T) {
	osRelease := "NAME=\"Ubuntu\"\nPRETTY_NAME=\"Ubuntu 24.04.1 LTS\"\nID=ubuntu\n"
	if got := parseOSReleasePrettyName(osRelease); got != "Ubuntu 24.04.1 LTS" {
		t.Errorf("parseOSReleasePrettyName = %q", got)
	}
}

func TestCollectDeviceInfoSane(t *testing.T) {
	d := collectDeviceInfo()
	if d.Cores < 1 {
		t.Errorf("Cores = %d", d.Cores)
	}
	if d.OSVersion == "" {
		t.Error("OSVersion empty")
	}
}
