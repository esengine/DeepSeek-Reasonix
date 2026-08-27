package main

import (
	"strings"
	"testing"
)

func TestReferenceRelativeSelection(t *testing.T) {
	base := "/workspace/project"

	tests := []struct {
		name    string
		path    string
		folder  bool
		want    string
		wantErr string
	}{
		{name: "file", path: base + "/config/local.json", want: "config/local.json"},
		{name: "hidden file", path: base + "/.env.local", want: ".env.local"},
		{name: "folder", path: base + "/generated", folder: true, want: "generated"},
		{name: "hidden folder", path: base + "/.cache", folder: true, want: ".cache"},
		{name: "outside", path: "/workspace/other/file.txt", wantErr: "inside the current workspace"},
		{name: "workspace folder", path: base, folder: true, wantErr: "workspace root cannot be hidden"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := referenceRelativeSelection(base, tt.path, tt.folder)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("referenceRelativeSelection: %v", err)
			}
			if got != tt.want {
				t.Fatalf("path = %q, want %q", got, tt.want)
			}
		})
	}
}
