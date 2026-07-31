package debme

import (
	"embed"
	"github.com/leaanthony/slicer"
	"github.com/matryer/is"
	"io"
	"io/fs"
	"os"
	"testing"
)

//go:embed fixtures
var testfixtures embed.FS

func TestReadFile(t *testing.T) {
	is2 := is.New(t)
	d, err := FS(testfixtures, "fixtures")
	is2.NoErr(err)
	file, err := d.ReadFile("test1/onefile.txt")
	is2.NoErr(err)
	is2.Equal(string(file), "test")
	test2, err := d.FS("test2")
	is2.NoErr(err)
	file, err = test2.ReadFile("inner/deeper/three.txt")
	is2.NoErr(err)
	is2.Equal(string(file), "3")

	_, err = FS(testfixtures, "badfixture")
	is2.True(err != nil)

	_, err = d.ReadFile("badfile")
	is2.True(err != nil)
}

func TestReadDir(t *testing.T) {
	is2 := is.New(t)
	d, err := FS(testfixtures, "fixtures/test2")
	is2.NoErr(err)
	files, err := d.ReadDir("inner")
	is2.NoErr(err)
	is2.Equal(len(files), 3)

	expectedFiles := slicer.String([]string{
		"deeper",
		"one.txt",
		"two.txt",
	})
	for _, file := range files {
		is2.True(expectedFiles.Contains(file.Name()))
	}

	_, err = d.ReadDir("baddir")
	is2.True(err != nil)
}

func TestOpen(t *testing.T) {
	is2 := is.New(t)
	d, err := FS(testfixtures, "fixtures/test1")
	is2.NoErr(err)
	file, err := d.Open("onefile.txt")
	is2.NoErr(err)
	data, err := io.ReadAll(file)
	is2.NoErr(err)
	is2.Equal(string(data), "test")

	_, err = d.Open("badfile")
	is2.True(err != nil)
}

func TestCompatibility(t *testing.T) {
	is2 := is.New(t)
	inner, err := FS(testfixtures, "fixtures/test2/inner")
	is2.NoErr(err)
	expectedFiles := slicer.String([]string{
		".",
		"deeper",
		"deeper/three.txt",
		"one.txt",
		"two.txt",
	})
	err = fs.WalkDir(inner, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		is2.True(expectedFiles.Contains(path))
		return nil
	})
	is2.NoErr(err)
}

func TestFS(t *testing.T) {
	is2 := is.New(t)
	_, err := FS(testfixtures, "baddir")
	is2.True(err != nil)
}

func TestCopy(t *testing.T) {
	is2 := is.New(t)
	inner, err := FS(testfixtures, "fixtures/test2/inner")
	is2.NoErr(err)
	err = inner.CopyFile("one.txt", "one.txt", 0644)
	is2.NoErr(err)
	sourceData, err := inner.ReadFile("one.txt")
	is2.NoErr(err)
	targetData, err := os.ReadFile("one.txt")
	is2.NoErr(err)
	is2.Equal(sourceData, targetData)

	// Bad source file
	err = inner.CopyFile("one.txtsd", "one.txt", 0644)
	is2.True(err != nil)

	// Bad target file
	err = inner.CopyFile("one.txt", "/:09/one.txt", 0644)
	is2.True(err != nil)

}
