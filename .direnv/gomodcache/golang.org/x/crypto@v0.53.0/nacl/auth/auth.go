// Copyright 2017 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package auth authenticates a message using a secret key.
//
// This package is interoperable with [NaCl].
//
// The auth package is essentially a wrapper for HMAC-SHA-512 (implemented by
// crypto/hmac and crypto/sha512), truncated to 32 bytes. It is [frozen] and is
// not accepting new features.
//
// [NaCl]: https://nacl.cr.yp.to/auth.html
// [frozen]: https://go.dev/wiki/Frozen
package auth

import (
	"crypto/hmac"
	"crypto/sha512"
)

const (
	// Size is the size, in bytes, of an authenticated digest.
	Size = 32
	// KeySize is the size, in bytes, of an authentication key.
	KeySize = 32
)

// Sum generates an authenticator for m using a secret key and returns the
// 32-byte digest.
func Sum(m []byte, key *[KeySize]byte) *[Size]byte {
	mac := hmac.New(sha512.New, key[:])
	mac.Write(m)
	out := new([Size]byte)
	copy(out[:], mac.Sum(nil)[:Size])
	return out
}

// Verify checks that digest is a valid authenticator of message m under the
// given secret key. Verify does not leak timing information.
func Verify(digest []byte, m []byte, key *[KeySize]byte) bool {
	if len(digest) != Size {
		return false
	}
	mac := hmac.New(sha512.New, key[:])
	mac.Write(m)
	expectedMAC := mac.Sum(nil) // first 256 bits of 512-bit sum
	return hmac.Equal(digest, expectedMAC[:Size])
}
