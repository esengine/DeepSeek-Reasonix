package sftp

import (
	"testing"
)

func TestServer_toLocalPath(t *testing.T) {
	tests := []struct {
		name        string
		withWorkDir string
		p           string
		want        string
	}{
		{
			name: "empty path with no workdir",
			p:    "",
			want: "",
		},
		{
			name: "relative path with no workdir",
			p:    "file",
			want: "file",
		},
		{
			name: "absolute path with no workdir",
			p:    "/file",
			want: "\\file",
		},
		{
			name:        "workdir and empty path",
			withWorkDir: "C:\\Users\\User",
			p:           "",
			want:        "C:\\Users\\User",
		},
		{
			name:        "workdir and relative path",
			withWorkDir: "C:\\Users\\User",
			p:           "file",
			want:        "C:\\Users\\User\\file",
		},
		{
			name:        "workdir and relative path with .",
			withWorkDir: "C:\\Users\\User",
			p:           ".",
			want:        "C:\\Users\\User",
		},
		{
			name:        "workdir and relative path with . and file",
			withWorkDir: "C:\\Users\\User",
			p:           "./file",
			want:        "C:\\Users\\User\\file",
		},
		{
			name:        "workdir and absolute path",
			withWorkDir: "C:\\Users\\User",
			p:           "/C:/file",
			want:        "C:\\file",
		},
		{
			name:        "workdir and non-unixy path prefixes workdir",
			withWorkDir: "C:\\Users\\User",
			p:           "C:\\file",
			// This may look like a bug but it is the result of passing
			// invalid input (a non-unixy path) to the server.
			want: "C:\\Users\\User\\C:\\file",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// We don't need to initialize the Server further to test
			// toLocalPath behavior.
			s := &Server{}
			if tt.withWorkDir != "" {
				if err := WithServerWorkingDirectory(tt.withWorkDir)(s); err != nil {
					t.Fatal(err)
				}
			}

			if got := s.toLocalPath(tt.p); got != tt.want {
				t.Errorf("Server.toLocalPath() = %q, want %q", got, tt.want)
			}
		})
	}
}
