//go:build 1.18
// +build 1.18

package ssh_config

import (
	"bytes"
	"testing"
)

func FuzzDecode(f *testing.F) {
	f.Fuzz(func(t *testing.T, in []byte) {
		_, err := Decode(bytes.NewReader(in))
		if err != nil {
			t.Fatalf("decode %q: %v", string(in), err)
		}
	})
}
