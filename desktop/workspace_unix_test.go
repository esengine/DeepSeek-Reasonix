//go:build unix

package main

import (
	"os"
	"syscall"
	"testing"
)

func TestReadFileRejectsNonRegularFile(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo("pipe", 0o600); err != nil {
		t.Fatal(err)
	}

	preview := (&App{}).ReadFile("pipe")
	if preview.Err != "path is not a regular file" {
		t.Fatalf("ReadFile fifo err = %q, want non-regular file error", preview.Err)
	}
}

func TestListDirOmitsNonRegularFiles(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("regular.txt", []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo("pipe", 0o600); err != nil {
		t.Fatal(err)
	}

	entries := (&App{}).ListDir("")
	seenRegular := false
	for _, entry := range entries {
		if entry.Name == "pipe" {
			t.Fatal("ListDir returned fifo as a file entry")
		}
		if entry.Name == "regular.txt" {
			seenRegular = true
		}
	}
	if !seenRegular {
		t.Fatal("ListDir did not return regular file")
	}
}
