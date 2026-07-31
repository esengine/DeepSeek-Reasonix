// Copyright (c) 2021 Andreas Auernhammer. All rights reserved.
// Use of this source code is governed by a license that can be
// found in the LICENSE file.

package minisign

import (
	"crypto"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const publicKeySize = 2 + 8 + ed25519.PublicKeySize

// PublicKeyFromFile reads a PublicKey from the given file.
func PublicKeyFromFile(filename string) (PublicKey, error) {
	bytes, err := os.ReadFile(filename)
	if err != nil {
		return PublicKey{}, err
	}

	var key PublicKey
	if err = key.UnmarshalText(bytes); err != nil {
		return PublicKey{}, err
	}
	return key, nil
}

// PublicKey is a minisign public key.
//
// A public key is used to verify whether messages
// have been signed with the corresponding private
// key.
type PublicKey struct {
	_ [0]func() // prevent direct comparison: p1 == p2.

	id    uint64
	bytes [ed25519.PublicKeySize]byte
}

// ID returns the 64 bit key ID.
func (p PublicKey) ID() uint64 { return p.id }

// Equal returns true if and only if p and x have equivalent values.
func (p PublicKey) Equal(x crypto.PublicKey) bool {
	xx, ok := x.(PublicKey)
	if !ok {
		return false
	}
	return p.id == xx.id && p.bytes == xx.bytes
}

// String returns a base64 string representation of the PublicKey p.
func (p PublicKey) String() string {
	var bytes [publicKeySize]byte
	binary.LittleEndian.PutUint16(bytes[:2], EdDSA)
	binary.LittleEndian.PutUint64(bytes[2:10], p.ID())
	copy(bytes[10:], p.bytes[:])

	return base64.StdEncoding.EncodeToString(bytes[:])
}

// MarshalText returns a textual representation of the PublicKey p.
//
// It never returns an error.
func (p PublicKey) MarshalText() ([]byte, error) {
	s := make([]byte, 0, 113) // Size of a public key in text format
	s = append(s, "untrusted comment: minisign public key: "...)
	s = append(s, strings.ToUpper(strconv.FormatUint(p.ID(), 16))...)
	s = append(s, '\n')
	s = append(s, p.String()...)
	return s, nil
}

// UnmarshalText decodes a textual representation of a public key into p.
//
// It returns an error in case of a malformed key.
func (p *PublicKey) UnmarshalText(text []byte) error {
	text = trimUntrustedComment(text)
	bytes := make([]byte, base64.StdEncoding.DecodedLen(len(text)))
	n, err := base64.StdEncoding.Decode(bytes, text)
	if err != nil {
		return fmt.Errorf("minisign: invalid public key: %v", err)
	}
	bytes = bytes[:n] // Adjust since text may contain '\r' or '\n' which would have been ignored during decoding.

	if n = len(bytes); n != publicKeySize {
		return errors.New("minisign: invalid public key length " + strconv.Itoa(n))
	}
	if a := binary.LittleEndian.Uint16(bytes[:2]); a != EdDSA {
		return errors.New("minisign: invalid public key algorithm " + strconv.Itoa(int(a)))
	}

	p.id = binary.LittleEndian.Uint64(bytes[2:10])
	copy(p.bytes[:], bytes[10:])
	return nil
}
