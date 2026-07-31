package binarydist

import (
	"io/ioutil"
	"os"
	"os/exec"
	"testing"
)

func TestPatch(t *testing.T) {
	mustWriteRandFile("test.old", 1e3)
	mustWriteRandFile("test.new", 1e3)

	got, err := ioutil.TempFile("/tmp", "bspatch.")
	if err != nil {
		panic(err)
	}
	os.Remove(got.Name())

	err = exec.Command("bsdiff", "test.old", "test.new", "test.patch").Run()
	if err != nil {
		panic(err)
	}

	err = Patch(mustOpen("test.old"), got, mustOpen("test.patch"))
	if err != nil {
		t.Fatal("err", err)
	}

	ref, err := got.Seek(0, 2)
	if err != nil {
		panic(err)
	}

	t.Logf("got %d bytes", ref)
	if n := fileCmp(got, mustOpen("test.new")); n > -1 {
		t.Fatalf("produced different output at pos %d", n)
	}
}

func TestPatchHk(t *testing.T) {
	got, err := ioutil.TempFile("/tmp", "bspatch.")
	if err != nil {
		panic(err)
	}
	os.Remove(got.Name())

	err = Patch(mustOpen("testdata/sample.old"), got, mustOpen("testdata/sample.patch"))
	if err != nil {
		t.Fatal("err", err)
	}

	ref, err := got.Seek(0, 2)
	if err != nil {
		panic(err)
	}

	t.Logf("got %d bytes", ref)
	if n := fileCmp(got, mustOpen("testdata/sample.new")); n > -1 {
		t.Fatalf("produced different output at pos %d", n)
	}
}
