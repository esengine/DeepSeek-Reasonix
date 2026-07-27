package sandbox

import (
	"testing"
)

func TestSandboxErrorDiagnostic(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		wantCap string // substring we expect in the diagnostic, or "" for no match
	}{
		{
			name:    "nvidia driver",
			output:  "error: command exited: exit status 9\ncouldn't communicate with the NVIDIA driver",
			wantCap: "devices",
		},
		{
			name:    "nvidia smi failed",
			output:  "NVIDIA-SMI has failed because it couldn't communicate with the NVIDIA driver",
			wantCap: "devices",
		},
		{
			name:    "unable to open database",
			output:  "[ERR_SQLITE_ERROR] unable to open database file\npnpm: unable to open database file",
			wantCap: "write_paths",
		},
		{
			name:    "read-only file system",
			output:  "mkdir: cannot create directory '/foo': Read-only file system",
			wantCap: "write_paths",
		},
		{
			name:    "chinese read-only",
			output:  "mkdir: 无法创建目录'/foo': 只读文件系统",
			wantCap: "write_paths",
		},
		{
			name:    "no write access",
			output:  "[ERROR] The CLI has no write access to the global bin directory",
			wantCap: "write_paths",
		},
		{
			name:    "permission denied",
			output:  "mkdir: cannot create directory '/foo': Permission denied",
			wantCap: "write_paths",
		},
		{
			name:    "connection timed out",
			output:  "curl: (28) Connection timed out after 10000 milliseconds",
			wantCap: "network",
		},
		{
			name:    "dns failure",
			output:  "curl: (6) Could not resolve host: example.com; Temporary failure in name resolution",
			wantCap: "network",
		},
		{
			name:    "no match - ordinary error",
			output:  "bash: foo: command not found",
			wantCap: "",
		},
		{
			name:    "no match - success output",
			output:  "Hello, World!",
			wantCap: "",
		},
		{
			name:    "no match - empty output",
			output:  "",
			wantCap: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SandboxErrorDiagnostic(tt.output)
			if tt.wantCap == "" {
				if got != "" {
					t.Errorf("SandboxErrorDiagnostic(%q) = %q, want empty", tt.output, got)
				}
				return
			}
			if got == "" {
				t.Errorf("SandboxErrorDiagnostic(%q) = empty, want diagnostic containing %q", tt.output, tt.wantCap)
				return
			}
			if !contains(t, got, tt.wantCap) {
				t.Errorf("SandboxErrorDiagnostic(%q) = %q, want containing %q", tt.output, got, tt.wantCap)
			}
			// Verify format structure
			if !contains(t, got, "sandbox diagnostic") {
				t.Errorf("diagnostic missing header/footer markers: %q", got)
			}
		})
	}
}

func contains(t *testing.T, s, substr string) bool {
	t.Helper()
	return len(substr) == 0 || len(s) >= len(substr) && containsStr(s, substr)
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
