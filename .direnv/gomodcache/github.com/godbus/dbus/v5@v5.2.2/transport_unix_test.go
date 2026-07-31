package dbus

import (
	"os"
	"testing"
)

const testString = `This is a test!
This text should be read from the file that is created by this test.`

type unixFDTest struct {
	t *testing.T
}

func (t unixFDTest) Testfd(fd UnixFD) (string, *Error) {
	var b [4096]byte
	file := os.NewFile(uintptr(fd), "testfile")
	defer file.Close()
	n, err := file.Read(b[:])
	if err != nil {
		return "", &Error{"com.github.guelfey.test.Error", nil}
	}
	return string(b[:n]), nil
}

func (t unixFDTest) Testvariant(v Variant) (string, *Error) {
	var b [4096]byte
	fd := v.Value().(UnixFD)
	file := os.NewFile(uintptr(fd), "testfile")
	defer file.Close()
	n, err := file.Read(b[:])
	if err != nil {
		return "", &Error{"com.github.guelfey.test.Error", nil}
	}
	return string(b[:n]), nil
}

type unixfdContainer struct {
	Fd UnixFD
}

func (t unixFDTest) Teststruct(s unixfdContainer) (string, *Error) {
	var b [4096]byte
	file := os.NewFile(uintptr(s.Fd), "testfile")
	defer file.Close()
	n, err := file.Read(b[:])
	if err != nil {
		return "", &Error{"com.github.guelfey.test.Error", nil}
	}
	return string(b[:n]), nil
}

func (t unixFDTest) Testvariantstruct(vs Variant) (string, *Error) {
	var b [4096]byte
	s := vs.Value().([]any)
	u := s[0].(UnixFD)
	file := os.NewFile(uintptr(u), "testfile")
	defer file.Close()
	n, err := file.Read(b[:])
	if err != nil {
		return "", &Error{"com.github.guelfey.test.Error", nil}
	}
	return string(b[:n]), nil
}

type variantContainer struct {
	V Variant
}

func (t unixFDTest) Teststructvariant(sv variantContainer) (string, *Error) {
	var b [4096]byte
	fd := sv.V.Value().(UnixFD)
	file := os.NewFile(uintptr(fd), "testfile")
	defer file.Close()
	n, err := file.Read(b[:])
	if err != nil {
		return "", &Error{"com.github.guelfey.test.Error", nil}
	}
	return string(b[:n]), nil
}

type unixFDTestBench struct {
	b *testing.B
}

func (bm unixFDTestBench) Testfd(fd UnixFD) (string, *Error) {
	var b [4096]byte
	file := os.NewFile(uintptr(fd), "testfile")
	defer file.Close()
	n, err := file.Read(b[:])
	if err != nil {
		return "", &Error{"com.github.guelfey.test.Error", nil}
	}
	return string(b[:n]), nil
}

func TestUnixFDs(t *testing.T) {
	conn, err := ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	name := conn.Names()[0]
	test := unixFDTest{t}
	err = conn.Export(test, "/com/github/guelfey/test", "com.github.guelfey.test")
	if err != nil {
		t.Fatal(err)
	}

	var s string
	obj := conn.Object(name, "/com/github/guelfey/test")

	if _, err := w.Write([]byte(testString)); err != nil {
		t.Fatal(err)
	}
	err = obj.Call("com.github.guelfey.test.Testfd", 0, UnixFD(r.Fd())).Store(&s)
	if err != nil {
		t.Fatal(err)
	}
	if s != testString {
		t.Fatal("got", s, "wanted", testString)
	}

	if _, err := w.Write([]byte(testString)); err != nil {
		t.Fatal(err)
	}
	err = obj.Call("com.github.guelfey.test.Testvariant", 0, MakeVariant(UnixFD(r.Fd()))).Store(&s)
	if err != nil {
		t.Fatal(err)
	}
	if s != testString {
		t.Fatal("got", s, "wanted", testString)
	}

	if _, err := w.Write([]byte(testString)); err != nil {
		t.Fatal(err)
	}
	err = obj.Call("com.github.guelfey.test.Teststruct", 0, unixfdContainer{UnixFD(r.Fd())}).Store(&s)
	if err != nil {
		t.Fatal(err)
	}
	if s != testString {
		t.Fatal("got", s, "wanted", testString)
	}

	if _, err := w.Write([]byte(testString)); err != nil {
		t.Fatal(err)
	}
	err = obj.Call("com.github.guelfey.test.Testvariantstruct", 0, MakeVariant(unixfdContainer{UnixFD(r.Fd())})).Store(&s)
	if err != nil {
		t.Fatal(err)
	}
	if s != testString {
		t.Fatal("got", s, "wanted", testString)
	}

	if _, err := w.Write([]byte(testString)); err != nil {
		t.Fatal(err)
	}
	err = obj.Call("com.github.guelfey.test.Teststructvariant", 0, variantContainer{MakeVariant(UnixFD(r.Fd()))}).Store(&s)
	if err != nil {
		t.Fatal(err)
	}
	if s != testString {
		t.Fatal("got", s, "wanted", testString)
	}
}

func BenchmarkUnixFDs(b *testing.B) {
	conn, err := ConnectSessionBus()
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := conn.Close(); err != nil {
			b.Error(err)
		}
	})
	r, w, err := os.Pipe()
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := w.Close(); err != nil {
			b.Error(err)
		}
	})
	name := conn.Names()[0]
	test := unixFDTestBench{b}
	err = conn.Export(test, "/com/github/guelfey/test", "com.github.guelfey.test")
	if err != nil {
		b.Fatal(err)
	}

	var s string
	obj := conn.Object(name, "/com/github/guelfey/test")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := w.Write([]byte(testString)); err != nil {
			b.Fatal(err)
		}
		err = obj.Call("com.github.guelfey.test.Testfd", 0, UnixFD(r.Fd())).Store(&s)
		if err != nil {
			b.Fatal(err)
		}
		if s != testString {
			b.Fatal("got", s, "wanted", testString)
		}
	}
}
