package main

import (
	"os"
	"path/filepath"
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

func TestSearchFileRefsSurfacesExactlyTypedHiddenEntry(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	dir := robustTempDir(t)
	if err := os.MkdirAll(filepath.Join(dir, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Thumbs.db"), []byte("noise"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Thumbs.db.bak"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	app := &App{}
	got := app.SearchFileRefs("Thumbs.db")
	if !hiddenDirEntry(got, "Thumbs.db") {
		t.Fatalf("exactly typed hidden file should surface marked Hidden, got %+v", got)
	}
	if hasDirEntry(app.SearchFileRefs("Thumbs.db.bak"), "Thumbs.db") {
		t.Fatalf("non-exact query must not surface hidden file, got %+v", app.SearchFileRefs("Thumbs.db.bak"))
	}
	if hasDirEntry(app.ListDir(""), "dist") {
		t.Fatalf("ListDir must keep omitting hidden dir, got %+v", app.ListDir(""))
	}
	if !hiddenDirEntry(app.SearchFileRefs("dist"), "dist") {
		t.Fatalf("exactly typed built-in hidden dir should be marked Hidden, got %+v", app.SearchFileRefs("dist"))
	}
}

func hiddenDirEntry(entries []DirEntry, name string) bool {
	for _, entry := range entries {
		if entry.Name == name {
			return entry.Hidden
		}
	}
	return false
}
