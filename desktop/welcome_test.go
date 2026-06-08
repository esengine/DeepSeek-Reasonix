package main

import (
	"encoding/json"
	"testing"
)

func TestGenerateChinesePromptsFromFiles(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		check func(t *testing.T, prompts []string)
	}{
		{
			name:  "Go project with go.mod",
			files: []string{"go.mod", "main.go", "internal/", "README.md"},
			check: func(t *testing.T, prompts []string) {
				if len(prompts) != 3 {
					t.Fatalf("want 3 prompts, got %d", len(prompts))
				}
				for i, p := range prompts {
					if p == "" {
						t.Errorf("prompt %d is empty", i)
					}
				}
			},
		},
		{
			name:  "Node project with package.json",
			files: []string{"package.json", "src/", "tsconfig.json", "node_modules/"},
			check: func(t *testing.T, prompts []string) {
				if len(prompts) != 3 {
					t.Fatalf("want 3 prompts, got %d", len(prompts))
				}
			},
		},
		{
			name:  "Notes folder with markdown",
			files: []string{"读书笔记.md", "2024年度总结.md", "旅行计划/"},
			check: func(t *testing.T, prompts []string) {
				if len(prompts) != 3 {
					t.Fatalf("want 3 prompts, got %d", len(prompts))
				}
				for i, p := range prompts {
					if p == "" {
						t.Errorf("prompt %d is empty", i)
					}
				}
			},
		},
		{
			name:  "Empty file list",
			files: []string{},
			check: func(t *testing.T, prompts []string) {
				if len(prompts) != 3 {
					t.Fatalf("want 3 prompts, got %d", len(prompts))
				}
			},
		},
		{
			name:  "Docker project",
			files: []string{"Dockerfile", "docker-compose.yml", "app.py", "requirements.txt"},
			check: func(t *testing.T, prompts []string) {
				if len(prompts) != 3 {
					t.Fatalf("want 3 prompts, got %d", len(prompts))
				}
			},
		},
		{
			name:  "Rust project",
			files: []string{"Cargo.toml", "src/main.rs", "Cargo.lock"},
			check: func(t *testing.T, prompts []string) {
				if len(prompts) != 3 {
					t.Fatalf("want 3 prompts, got %d", len(prompts))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompts := generateChinesePromptsFromFiles(tt.files)
			tt.check(t, prompts)
		})
	}
}

func TestGenerateChinesePromptsFromFilesRandomness(t *testing.T) {
	files := []string{"go.mod", "main.go", "internal/", "README.md", "Dockerfile"}
	seen := make(map[string]bool)
	for i := 0; i < 20; i++ {
		prompts := generateChinesePromptsFromFiles(files)
		key, _ := json.Marshal(prompts)
		seen[string(key)] = true
	}
	// With multiple candidates per slot, we should see more than 1 unique
	// combination across 20 calls.
	if len(seen) < 2 {
		t.Errorf("expected varied prompts across 20 calls, got %d unique combinations", len(seen))
	}
}

func TestGenerateChinesePromptsFromFilesAlwaysThree(t *testing.T) {
	// Every result must be exactly 3 non-empty strings.
	cases := [][]string{
		{"a.go", "b.go"},
		{"README.md"},
		{"dir1/", "dir2/", "file.txt", "Makefile", "setup.py"},
		{},
	}
	for _, files := range cases {
		prompts := generateChinesePromptsFromFiles(files)
		if len(prompts) != 3 {
			t.Fatalf("files=%v: want 3 prompts, got %d", files, len(prompts))
		}
		for i, p := range prompts {
			if p == "" {
				t.Errorf("files=%v: prompt %d is empty", files, i)
			}
		}
	}
}
