package random

import (
	"bufio"
	"crypto/rand"
	"io"
	"strings"
	"sync"
)

type (
	Random struct {
		readerPool sync.Pool
	}
)

// Charsets
const (
	Uppercase    = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	Lowercase    = "abcdefghijklmnopqrstuvwxyz"
	Alphabetic   = Uppercase + Lowercase
	Numeric      = "0123456789"
	Alphanumeric = Alphabetic + Numeric
	Symbols      = "`" + `~!@#$%^&*()-_+={}[]|\;:"<>,./?`
	Hex          = Numeric + "abcdef"
)

var (
	global = New()
)

func New() *Random {
	// https://tip.golang.org/doc/go1.19#:~:text=Read%20no%20longer%20buffers%20random%20data%20obtained%20from%20the%20operating%20system%20between%20calls
	// sync.Pool must not be copied after first use; construct it directly
	// on the struct to avoid copying from a local var.
	return &Random{
		readerPool: sync.Pool{
			New: func() interface{} {
				return bufio.NewReader(rand.Reader)
			},
		},
	}
}

func (r *Random) String(length uint8, charsets ...string) string {
	charset := strings.Join(charsets, "")
	if charset == "" {
		charset = Alphanumeric
	}

	charsetLen := len(charset)
	if charsetLen > 255 {
		charsetLen = 255
	}
	maxByte := 255 - (256 % charsetLen)

	reader := r.readerPool.Get().(*bufio.Reader)
	defer r.readerPool.Put(reader)

	b := make([]byte, length)
	rs := make([]byte, length+(length/4)) // perf: avoid read from rand.Reader many times
	var i uint8 = 0

	// security note:
	// we can't just simply do b[i]=charset[rb%byte(charsetLen)],
	// for example, when charsetLen is 52, and rb is [0, 255], 256 = 52 * 4 + 48.
	// this will make the first 48 characters more possibly to be generated then others.
	// so we have to skip bytes when rb > maxByte

	for {
		_, err := io.ReadFull(reader, rs)
		if err != nil {
			panic("unexpected error happened when reading from bufio.NewReader(crypto/rand.Reader)")
		}
		for _, rb := range rs {
			if rb > byte(maxByte) {
				// Skip this number to avoid bias.
				continue
			}
			b[i] = charset[rb%byte(charsetLen)]
			i++
			if i == length {
				return string(b)
			}
		}
	}
}

func String(length uint8, charsets ...string) string {
	return global.String(length, charsets...)
}
