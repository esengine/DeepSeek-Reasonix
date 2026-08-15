package main

import "testing"

func TestSanitizeRemoteURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		// HTTPS URLs (the clean path)
		{
			name: "simple https",
			raw:  "https://github.com/org/repo.git",
			want: "https://github.com/org/repo",
		},
		{
			name: "https without .git",
			raw:  "https://github.com/org/repo",
			want: "https://github.com/org/repo",
		},
		{
			name: "https trailing slash",
			raw:  "https://github.com/org/repo/",
			want: "https://github.com/org/repo",
		},
		{
			name: "https trailing .git and slash",
			raw:  "https://github.com/org/repo.git/",
			want: "https://github.com/org/repo",
		},
		{
			name: "https with credentials stripped",
			raw:  "https://user:ghp_token123@github.com/org/repo.git",
			want: "https://github.com/org/repo",
		},
		{
			name: "https with username only stripped",
			raw:  "https://user@github.com/org/repo.git",
			want: "https://github.com/org/repo",
		},
		{
			name: "https query dropped",
			raw:  "https://github.com/org/repo.git?token=abc",
			want: "https://github.com/org/repo",
		},
		{
			name: "https fragment dropped",
			raw:  "https://github.com/org/repo.git#readme",
			want: "https://github.com/org/repo",
		},
		{
			name: "uppercase .GIT preserved",
			raw:  "https://github.com/org/repo.GIT",
			want: "https://github.com/org/repo.GIT",
		},
		{
			name: "https host only",
			raw:  "https://github.com",
			want: "https://github.com",
		},
		// SCP-style SSH (git@host:path)
		{
			name: "scp-style ssh",
			raw:  "git@github.com:org/repo.git",
			want: "https://github.com/org/repo",
		},
		{
			name: "scp-style ssh without .git",
			raw:  "git@github.com:org/repo",
			want: "https://github.com/org/repo",
		},
		{
			name: "scp-style ssh trailing slash",
			raw:  "git@github.com:org/repo/",
			want: "https://github.com/org/repo",
		},
		{
			name: "scp-style ssh trailing .git and slash",
			raw:  "git@github.com:org/repo.git/",
			want: "https://github.com/org/repo",
		},
		{
			name: "scp-style ssh nested path",
			raw:  "git@gitlab.com:group/subgroup/project.git",
			want: "https://gitlab.com/group/subgroup/project",
		},
		{
			name: "scp-style with non-git user",
			raw:  "deploy@gitlab.com:group/subgroup/project.git",
			want: "https://gitlab.com/group/subgroup/project",
		},
		// ssh:// URL scheme
		{
			name: "ssh URL scheme",
			raw:  "ssh://git@github.com/org/repo.git",
			want: "https://github.com/org/repo",
		},
		{
			name: "ssh URL scheme no .git",
			raw:  "ssh://git@github.com/org/repo",
			want: "https://github.com/org/repo",
		},
		{
			name: "ssh URL scheme trailing .git and slash",
			raw:  "ssh://git@github.com/org/repo.git/",
			want: "https://github.com/org/repo",
		},
		{
			name: "ssh URL scheme with port",
			raw:  "ssh://git@github.com:2222/org/repo.git",
			want: "https://github.com:2222/org/repo",
		},
		{
			name: "ssh URL credentials stripped",
			raw:  "ssh://user:pass@github.com/org/repo.git",
			want: "https://github.com/org/repo",
		},
		// HTTP (non-HTTPS)
		{
			name: "http URL preserved",
			raw:  "http://git.example.com/org/repo.git",
			want: "http://git.example.com/org/repo",
		},
		// Invalid / edge cases — should return empty
		{
			name: "empty string",
			raw:  "",
			want: "",
		},
		{
			name: "just whitespace",
			raw:  "  ",
			want: "",
		},
		{
			name: "unknown protocol (ftp)",
			raw:  "ftp://git.example.com/repo",
			want: "",
		},
		{
			name: "malformed scp no colon",
			raw:  "git@github.com",
			want: "",
		},
		{
			name: "malformed scp empty host",
			raw:  "git@:org/repo.git",
			want: "",
		},
		{
			name: "scp userinfo injection",
			raw:  "git@user@host:org/repo.git",
			want: "",
		},
		{
			name: "https missing host",
			raw:  "https:///org/repo",
			want: "",
		},
		{
			name: "random text",
			raw:  "not a git url",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeRemoteURL(tt.raw)
			if got != tt.want {
				t.Errorf("sanitizeRemoteURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}
