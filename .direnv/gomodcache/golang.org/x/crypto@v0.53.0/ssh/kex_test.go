// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ssh

// Key exchange tests.

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"reflect"
	"sync"
	"testing"
)

// Runs multiple key exchanges concurrent to detect potential data races with
// kex obtained from the global kexAlgoMap.
// This test needs to be executed using the race detector in order to detect
// race conditions.
func TestKexes(t *testing.T) {
	type kexResultErr struct {
		result *kexResult
		err    error
	}

	for name, kex := range kexAlgoMap {
		t.Run(name, func(t *testing.T) {
			wg := sync.WaitGroup{}
			for i := 0; i < 3; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					a, b := memPipe()

					s := make(chan kexResultErr, 1)
					c := make(chan kexResultErr, 1)
					var magics handshakeMagics
					go func() {
						r, e := kex.Client(a, rand.Reader, &magics)
						a.Close()
						c <- kexResultErr{r, e}
					}()
					go func() {
						r, e := kex.Server(b, rand.Reader, &magics, testSigners["ecdsa"].(AlgorithmSigner), testSigners["ecdsa"].PublicKey().Type())
						b.Close()
						s <- kexResultErr{r, e}
					}()

					clientRes := <-c
					serverRes := <-s
					if clientRes.err != nil {
						t.Errorf("client: %v", clientRes.err)
					}
					if serverRes.err != nil {
						t.Errorf("server: %v", serverRes.err)
					}
					if !reflect.DeepEqual(clientRes.result, serverRes.result) {
						t.Errorf("kex %q: mismatch %#v, %#v", name, clientRes.result, serverRes.result)
					}
				}()
			}
			wg.Wait()
		})
	}
}

func TestChooseDH(t *testing.T) {
	oakley := map[int]string{
		2048: oakleyGroup14,
		3072: oakleyGroup15,
		4096: oakleyGroup16,
	}
	expected := func(size int) *big.Int {
		hex, ok := oakley[size]
		if !ok {
			t.Fatalf("test setup: no Oakley group for size %d", size)
		}
		p, _ := new(big.Int).SetString(hex, 16)
		return p
	}

	tests := []struct {
		name    string
		request kexDHGexRequestMsg
		want    int // expected bit size; 0 means error expected
		wantErr bool
	}{
		{
			name:    "Standard 2048 request",
			request: kexDHGexRequestMsg{MinBits: 1024, PreferredBits: 2048, MaxBits: 8192},
			want:    2048,
		},
		{
			name:    "Standard 3072 request",
			request: kexDHGexRequestMsg{MinBits: 1024, PreferredBits: 3072, MaxBits: 8192},
			want:    3072,
		},
		{
			name:    "Standard 4096 request",
			request: kexDHGexRequestMsg{MinBits: 1024, PreferredBits: 4096, MaxBits: 8192},
			want:    4096,
		},
		{
			name:    "Preferred 2500 -> Expect 3072 (round up)",
			request: kexDHGexRequestMsg{MinBits: 1024, PreferredBits: 2500, MaxBits: 8192},
			want:    3072,
		},
		{
			name:    "Preferred 3500 -> Expect 4096 (round up)",
			request: kexDHGexRequestMsg{MinBits: 1024, PreferredBits: 3500, MaxBits: 8192},
			want:    4096,
		},
		{
			name:    "Preferred too high (8000) -> Expect 4096 (cap at max available)",
			request: kexDHGexRequestMsg{MinBits: 1024, PreferredBits: 8000, MaxBits: 8192},
			want:    4096,
		},
		{
			name:    "No group in range",
			request: kexDHGexRequestMsg{MinBits: 2500, PreferredBits: 2500, MaxBits: 2900},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := chooseDH(tt.request)

			if (err != nil) != tt.wantErr {
				t.Errorf("chooseDH() error = %v, wantErr %t", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if got == nil {
				t.Fatalf("chooseDH() returned nil big.Int but expected success")
			}
			if got.BitLen() != tt.want {
				t.Errorf("chooseDH() got size = %d, want %d", got.BitLen(), tt.want)
			}
			if want := expected(tt.want); got.Cmp(want) != 0 {
				t.Errorf("chooseDH() returned the wrong group for size %d", tt.want)
			}
		})
	}
}

func BenchmarkKexes(b *testing.B) {
	type kexResultErr struct {
		result *kexResult
		err    error
	}

	for name, kex := range kexAlgoMap {
		b.Run(name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				t1, t2 := memPipe()

				s := make(chan kexResultErr, 1)
				c := make(chan kexResultErr, 1)
				var magics handshakeMagics

				go func() {
					r, e := kex.Client(t1, rand.Reader, &magics)
					t1.Close()
					c <- kexResultErr{r, e}
				}()
				go func() {
					r, e := kex.Server(t2, rand.Reader, &magics, testSigners["ecdsa"].(AlgorithmSigner), testSigners["ecdsa"].PublicKey().Type())
					t2.Close()
					s <- kexResultErr{r, e}
				}()

				clientRes := <-c
				serverRes := <-s

				if clientRes.err != nil {
					panic(fmt.Sprintf("client: %v", clientRes.err))
				}
				if serverRes.err != nil {
					panic(fmt.Sprintf("server: %v", serverRes.err))
				}
			}
		})
	}
}
