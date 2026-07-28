package main

import "testing"

func TestAllowEmbedBrowserURL(t *testing.T) {
	tests := []struct {
		raw     string
		want    string
		wantErr bool
	}{
		{"", "", false},
		{"  ", "", false},
		{"https://example.com/a", "https://example.com/a", false},
		{"http://localhost:3000/", "http://localhost:3000/", false},
		{"ftp://example.com", "", true},
		{"javascript:alert(1)", "", true},
		{"data:text/html,hi", "", true},
		{"not a url", "", true},
	}
	for _, tt := range tests {
		got, err := allowEmbedBrowserURL(tt.raw)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("allowEmbedBrowserURL(%q) err=nil, want error", tt.raw)
			}
			continue
		}
		if err != nil {
			t.Fatalf("allowEmbedBrowserURL(%q) unexpected err: %v", tt.raw, err)
		}
		if got != tt.want {
			t.Fatalf("allowEmbedBrowserURL(%q)=%q, want %q", tt.raw, got, tt.want)
		}
	}
}
