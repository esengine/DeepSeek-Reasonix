package binarydist

import (
	"crypto/rand"
	"io"
	"io/ioutil"
	"os"
)

func mustOpen(path string) *os.File {
	f, err := os.Open(path)
	if err != nil {
		panic(err)
	}

	return f
}

func mustReadAll(r io.Reader) []byte {
	b, err := ioutil.ReadAll(r)
	if err != nil {
		panic(err)
	}
	return b
}

func fileCmp(a, b *os.File) int64 {
	sa, err := a.Seek(0, 2)
	if err != nil {
		panic(err)
	}

	sb, err := b.Seek(0, 2)
	if err != nil {
		panic(err)
	}

	if sa != sb {
		return sa
	}

	_, err = a.Seek(0, 0)
	if err != nil {
		panic(err)
	}

	_, err = b.Seek(0, 0)
	if err != nil {
		panic(err)
	}

	pa, err := ioutil.ReadAll(a)
	if err != nil {
		panic(err)
	}

	pb, err := ioutil.ReadAll(b)
	if err != nil {
		panic(err)
	}

	for i := range pa {
		if pa[i] != pb[i] {
			return int64(i)
		}
	}
	return -1
}

func mustWriteRandFile(path string, size int) *os.File {
	p := make([]byte, size)
	_, err := rand.Read(p)
	if err != nil {
		panic(err)
	}

	f, err := os.Create(path)
	if err != nil {
		panic(err)
	}

	_, err = f.Write(p)
	if err != nil {
		panic(err)
	}

	_, err = f.Seek(0, 0)
	if err != nil {
		panic(err)
	}

	return f
}
