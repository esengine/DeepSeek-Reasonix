// Copyright 2015 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkcs12

import (
	"crypto/x509/pkix"
	"encoding/asn1"
	"testing"
)

func TestVerifyMacIterationLimit(t *testing.T) {
	password, _ := bmpString("Sesame open")
	message := []byte{11, 12, 13, 14, 15}

	tests := []struct {
		name       string
		iterations int
		wantErr    bool
	}{
		{"at limit", maxIterations, false},
		{"over limit", maxIterations + 1, true},
		{"negative", -1, true},
		{"max int", 1<<31 - 1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			td := macData{
				Mac: digestInfo{
					Algorithm: pkix.AlgorithmIdentifier{
						Algorithm: oidSHA1,
					},
					Digest: nil, // will fail MAC check, but iteration check comes first
				},
				MacSalt:    []byte{1, 2, 3, 4, 5, 6, 7, 8},
				Iterations: tt.iterations,
			}
			err := verifyMac(&td, message, password)
			if tt.wantErr {
				if _, ok := err.(NotImplementedError); !ok {
					t.Errorf("iterations=%d: got %v, want NotImplementedError", tt.iterations, err)
				}
			} else {
				if _, ok := err.(NotImplementedError); ok {
					t.Errorf("iterations=%d: got unexpected NotImplementedError", tt.iterations)
				}
			}
		})
	}
}

func TestVerifyMac(t *testing.T) {
	td := macData{
		Mac: digestInfo{
			Digest: []byte{0x18, 0x20, 0x3d, 0xff, 0x1e, 0x16, 0xf4, 0x92, 0xf2, 0xaf, 0xc8, 0x91, 0xa9, 0xba, 0xd6, 0xca, 0x9d, 0xee, 0x51, 0x93},
		},
		MacSalt:    []byte{1, 2, 3, 4, 5, 6, 7, 8},
		Iterations: 2048,
	}

	message := []byte{11, 12, 13, 14, 15}
	password, _ := bmpString("")

	td.Mac.Algorithm.Algorithm = asn1.ObjectIdentifier([]int{1, 2, 3})
	err := verifyMac(&td, message, password)
	if _, ok := err.(NotImplementedError); !ok {
		t.Errorf("err: %v", err)
	}

	td.Mac.Algorithm.Algorithm = asn1.ObjectIdentifier([]int{1, 3, 14, 3, 2, 26})
	err = verifyMac(&td, message, password)
	if err != ErrIncorrectPassword {
		t.Errorf("Expected incorrect password, got err: %v", err)
	}

	password, _ = bmpString("Sesame open")
	err = verifyMac(&td, message, password)
	if err != nil {
		t.Errorf("err: %v", err)
	}

}
