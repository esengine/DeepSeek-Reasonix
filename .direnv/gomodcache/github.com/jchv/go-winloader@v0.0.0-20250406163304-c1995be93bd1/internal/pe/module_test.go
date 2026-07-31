package pe

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestLoadNothing(t *testing.T) {
	_, err := LoadModule(bytes.NewReader([]byte{}))
	if err != io.EOF {
		t.Errorf("expected EOF got %v", err)
	}
}

func TestLoadBadMZ(t *testing.T) {
	_, err := LoadModule(strings.NewReader("This string is long enough to be an MZ executable, but it isn't."))
	if err != ErrBadMZSignature {
		t.Errorf("expected ErrBadMZSignature got %v", err)
	}
}

func TestLoadBadPE(t *testing.T) {
	_, err := LoadModule(strings.NewReader("MZ, is what I'd say if I were an MZ executable, but I'm not!\000\000\000\000"))
	if err != ErrBadPESignature {
		t.Errorf("expected ErrBadPESignature got %v", err)
	}
}
func TestLoadTinyPE32(t *testing.T) {
	f, err := os.Open("../../tinydll/tiny.dll")
	if err != nil {
		t.Fatal(err)
	}
	_, err = LoadModule(f)
	if err != nil {
		t.Fatalf("expected nil error got %v", err)
	}
}
