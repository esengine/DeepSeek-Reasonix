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

func TestParseOSReleaseTechnicalFields(t *testing.T) {
	fields := parseOSRelease("# comment\nID=fedora\nVERSION_ID=\"40\"\nPRETTY_NAME='Fedora Linux 40'\nBROKEN\n")
	if fields["ID"] != "fedora" || fields["VERSION_ID"] != "40" || fields["PRETTY_NAME"] != "Fedora Linux 40" {
		t.Fatalf("os-release fields = %#v", fields)
	}
}

func TestParseOSReleaseDistributionMatrix(t *testing.T) {
	tests := []struct {
		name, body, id, version string
	}{
		{name: "ubuntu", body: "ID=ubuntu\nVERSION_ID=22.04\n", id: "ubuntu", version: "22.04"},
		{name: "debian", body: "ID=debian\nVERSION_ID=12\n", id: "debian", version: "12"},
		{name: "fedora", body: "ID=fedora\nVERSION_ID=40\n", id: "fedora", version: "40"},
		{name: "arch", body: "ID=arch\n", id: "arch"},
		{name: "unknown", body: "NAME=Custom Linux\n", id: ""},
		{name: "malformed", body: "BROKEN\n=bad\n", id: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fields := parseOSRelease(test.body)
			if fields["ID"] != test.id || fields["VERSION_ID"] != test.version {
				t.Fatalf("fields = %#v", fields)
			}
		})
	}
}

func TestLinuxSessionTypeUsesBoundedBuckets(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{name: "wayland", env: map[string]string{"XDG_SESSION_TYPE": "Wayland"}, want: "wayland"},
		{name: "x11 fallback", env: map[string]string{"DISPLAY": ":0"}, want: "x11"},
		{name: "remote wins", env: map[string]string{"SSH_CONNECTION": "present", "XDG_SESSION_TYPE": "wayland"}, want: "remote"},
		{name: "unknown", env: map[string]string{"XDG_SESSION_TYPE": "tty"}, want: "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := linuxSessionType(func(key string) string { return tt.env[key] }); got != tt.want {
				t.Fatalf("linuxSessionType() = %q, want %q", got, tt.want)
			}
		})
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
