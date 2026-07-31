package safety

import (
	"context"
	"testing"
)

func TestAssertPublicURL(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		url       string
		allowlist []string
		wantErr   bool
	}{
		{"http://example.com", nil, false},
		{"https://google.com", nil, false},
		{"http://127.0.0.1", nil, true},
		{"http://192.168.1.1", nil, true},
		{"http://10.0.0.1", nil, true},
		{"http://localhost", nil, true},
		{"http://[::1]", nil, true},
		{"http://169.254.169.254", nil, true},
		{"http://example.local", []string{"example.local"}, false},
		{"ftp://example.com", nil, true},
	}

	for _, tt := range tests {
		opts := &SsrfGuardOptions{Allowlist: tt.allowlist}
		err := AssertPublicURL(ctx, tt.url, opts)
		if (err != nil) != tt.wantErr {
			t.Errorf("AssertPublicURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
		}
	}
}
