package gosod

import (
	"embed"
	"github.com/leaanthony/debme"
	"github.com/matryer/is"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestNewFromDirFS(t *testing.T) {

	is2 := is.New(t)
	thisdir, err := os.Getwd()
	is2.NoErr(err)
	base := filepath.Join(thisdir, "internal/templatedir/testdata/embedded")
	testdir := os.DirFS(base)
	g := New(testdir)

	targetDir, err := ioutil.TempDir(".", "test_results")
	is2.NoErr(err)
	defer func(path string) {
		err := os.RemoveAll(path)
		is2.NoErr(err)
	}(targetDir)

	g.IgnoreFile("ignored.txt")

	type TestData struct {
		Name string
	}
	err = g.Extract(targetDir, &TestData{
		Name: "Roger Mellie",
	})
	is2.NoErr(err)

}

//go:embed internal/templatedir/testdata/embedded
var embeddedData embed.FS

func TestNewFromEmbed(t *testing.T) {

	is2 := is.New(t)

	// We use debme to create an FS from further down in the embedded directory
	fs, err := debme.FS(embeddedData, "internal/templatedir/testdata/embedded")
	is2.NoErr(err)
	g := New(fs)

	targetDir, err := ioutil.TempDir(".", "test_results")
	is2.NoErr(err)
	defer func(path string) {
		err := os.RemoveAll(path)
		is2.NoErr(err)
	}(targetDir)

	g.IgnoreFile("ignored.txt")
	g.SetTemplateFilters([]string{".filtername", ".tmpl"})

	type TestData struct {
		Name string
	}
	err = g.Extract(targetDir, &TestData{
		Name: "Roger Mellie",
	})
	is2.NoErr(err)
	expectedFiles := []string{
		"test.go",
		"subdir/included.txt",
		"subdir/sub.go",
		"custom.txt",
	}

	err = fstest.TestFS(os.DirFS(targetDir), expectedFiles...)
	is2.NoErr(err)
}
