package templatedir

import (
	"embed"
	"github.com/leaanthony/debme"
	"github.com/matryer/is"
	"os"
	"path/filepath"
	"testing"
)

//go:embed testdata/**
var embeddedData embed.FS

func TestNew(t *testing.T) {
	is2 := is.New(t)
	// We want an FS further down the embedded data
	fs, err := debme.FS(embeddedData, "testdata/embedded")
	is2.NoErr(err)
	templDir := New(fs)
	targetDir, err := os.MkdirTemp(".", "test_results")
	is2.NoErr(err)
	defer func(path string) {
		err := os.RemoveAll(path)
		is2.NoErr(err)
	}(targetDir)

	defer os.RemoveAll(targetDir)

	templDir.IgnoreFile("ignored.txt")
	templDir.SetTemplateFilters([]string{".filtername", ".tmpl"})

	type TestData struct {
		Name string
	}
	err = templDir.Extract(targetDir, &TestData{
		Name: "Roger Mellie",
	})
	is2.NoErr(err)
	expectedFiles := []string{
		"test.go",
		"subdir/included.txt",
		"subdir/sub.go",
		"custom.txt",
	}

	// Check that the files are there as expected
	for _, file := range expectedFiles {
		td, err := filepath.Abs(targetDir)
		is2.NoErr(err)
		_, err = os.Stat(filepath.Join(td, file))
		is2.NoErr(err)
	}
}

func TestNewNamed(t *testing.T) {
	is2 := is.New(t)
	// We want an FS further down the embedded data
	fs, err := debme.FS(embeddedData, "testdata/filenames")
	is2.NoErr(err)
	templDir := New(fs)
	targetDir, err := os.MkdirTemp(".", "test_results")
	is2.NoErr(err)
	defer func(path string) {
		err := os.RemoveAll(path)
		is2.NoErr(err)
	}(targetDir)

	defer os.RemoveAll(targetDir)

	templDir.IgnoreFile("ignored.txt")
	templDir.SetTemplateFilters([]string{".filtername", ".tmpl"})

	type TestData struct {
		Name string
	}
	err = templDir.Extract(targetDir, &TestData{
		Name: "newname",
	})
	is2.NoErr(err)
	expectedFiles := []string{
		"newname.go",
	}

	// Check that the files are there as expected
	for _, file := range expectedFiles {
		td, err := filepath.Abs(targetDir)
		is2.NoErr(err)
		_, err = os.Stat(filepath.Join(td, file))
		is2.NoErr(err)
	}
}
func TestNewNamed2(t *testing.T) {
	is2 := is.New(t)
	// We want an FS further down the embedded data
	fs, err := debme.FS(embeddedData, "testdata/dirnames")
	is2.NoErr(err)
	templDir := New(fs)
	targetDir, err := os.MkdirTemp(".", "test_results")
	is2.NoErr(err)
	defer func(path string) {
		err := os.RemoveAll(path)
		is2.NoErr(err)
	}(targetDir)

	defer os.RemoveAll(targetDir)

	templDir.IgnoreFile("ignored.txt")
	templDir.SetTemplateFilters([]string{".filtername", ".tmpl"})

	type TestData struct {
		Name string
	}
	err = templDir.Extract(targetDir, &TestData{
		Name: "newname",
	})
	is2.NoErr(err)
	expectedFiles := []string{
		"newname/normal.go",
	}

	// Check that the files are there as expected
	for _, file := range expectedFiles {
		td, err := filepath.Abs(targetDir)
		is2.NoErr(err)
		_, err = os.Stat(filepath.Join(td, file))
		is2.NoErr(err)
	}
}
