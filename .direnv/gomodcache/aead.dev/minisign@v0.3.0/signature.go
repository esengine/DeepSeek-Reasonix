// Copyright (c) 2021 Andreas Auernhammer. All rights reserved.
// Use of this source code is governed by a license that can be
// found in the LICENSE file.

package minisign

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// SignatureFromFile reads a Signature from the given file.
func SignatureFromFile(filename string) (Signature, error) {
	bytes, err := os.ReadFile(filename)
	if err != nil {
		return Signature{}, err
	}

	var signature Signature
	if err = signature.UnmarshalText(bytes); err != nil {
		return Signature{}, err
	}
	return signature, nil
}

// Signature is a structured representation of a minisign
// signature.
//
// A signature is generated when signing a message with
// a private key:
//
//	signature = Sign(privateKey, message)
//
// The signature of a message can then be verified with the
// corresponding public key:
//
//	if Verify(publicKey, message, signature) {
//	   // => signature is valid
//	   // => message has been signed with correspoding private key
//	}
type Signature struct {
	_ [0]func() // enforce named assignment and prevent direct comparison

	// Algorithm is the signature algorithm. It is either EdDSA or HashEdDSA.
	Algorithm uint16

	// KeyID may be the 64 bit ID of the private key that was used
	// to produce this signature. It can be used to identify the
	// corresponding public key that can verify the signature.
	//
	// However, key IDs are random identifiers and not protected at all.
	// A key ID is just a hint to quickly identify a public key candidate.
	KeyID uint64

	// TrustedComment is a comment that has been signed and is
	// verified during signature verification.
	TrustedComment string

	// UntrustedComment is a comment that has not been signed
	// and is not verified during signature verification.
	//
	// It must not be considered authentic - in contrast to the
	// TrustedComment.
	UntrustedComment string

	// Signature is the Ed25519 signature of the message that
	// has been signed.
	Signature [ed25519.SignatureSize]byte

	// CommentSignature is the Ed25519 signature of Signature
	// concatenated with the TrustedComment:
	//
	//    CommentSignature = ed25519.Sign(PrivateKey, Signature || TrustedComment)
	//
	// It is used to verify that the TrustedComment is authentic.
	CommentSignature [ed25519.SignatureSize]byte
}

// String returns a string representation of the Signature s.
//
// In contrast to MarshalText, String does not fail if s is
// not a valid minisign signature.
func (s Signature) String() string {
	return string(encodeSignature(&s))
}

// Equal reports whether s and x have equivalent values.
//
// The untrusted comments of two equivalent signatures may differ.
func (s Signature) Equal(x Signature) bool {
	return s.Algorithm == x.Algorithm &&
		s.KeyID == x.KeyID &&
		s.Signature == x.Signature &&
		s.CommentSignature == x.CommentSignature &&
		s.TrustedComment == x.TrustedComment
}

// MarshalText returns a textual representation of the Signature s.
//
// It returns an error if s cannot be a valid signature, for example.
// when s.Algorithm is neither EdDSA nor HashEdDSA.
func (s Signature) MarshalText() ([]byte, error) {
	if s.Algorithm != EdDSA && s.Algorithm != HashEdDSA {
		return nil, errors.New("minisign: invalid signature algorithm " + strconv.Itoa(int(s.Algorithm)))
	}
	return encodeSignature(&s), nil
}

// UnmarshalText decodes a textual representation of a signature into s.
//
// It returns an error in case of a malformed signature.
func (s *Signature) UnmarshalText(text []byte) error {
	segments := strings.SplitN(string(text), "\n", 4)
	if len(segments) != 4 {
		return errors.New("minisign: invalid signature")
	}

	var (
		untrustedComment        = strings.TrimRight(segments[0], "\r")
		encodedSignature        = segments[1]
		trustedComment          = strings.TrimRight(segments[2], "\r")
		encodedCommentSignature = segments[3]
	)
	if !strings.HasPrefix(untrustedComment, "untrusted comment: ") {
		return errors.New("minisign: invalid signature: invalid untrusted comment")
	}
	if !strings.HasPrefix(trustedComment, "trusted comment: ") {
		return errors.New("minisign: invalid signature: invalid trusted comment")
	}

	rawSignature, err := base64.StdEncoding.DecodeString(encodedSignature)
	if err != nil {
		return fmt.Errorf("minisign: invalid signature: %v", err)
	}
	if n := len(rawSignature); n != 2+8+ed25519.SignatureSize {
		return errors.New("minisign: invalid signature length " + strconv.Itoa(n))
	}
	commentSignature, err := base64.StdEncoding.DecodeString(encodedCommentSignature)
	if err != nil {
		return fmt.Errorf("minisign: invalid signature: %v", err)
	}
	if n := len(commentSignature); n != ed25519.SignatureSize {
		return errors.New("minisign: invalid comment signature length " + strconv.Itoa(n))
	}

	var (
		algorithm = binary.LittleEndian.Uint16(rawSignature[:2])
		keyID     = binary.LittleEndian.Uint64(rawSignature[2:10])
	)
	if algorithm != EdDSA && algorithm != HashEdDSA {
		return errors.New("minisign: invalid signature: invalid algorithm " + strconv.Itoa(int(algorithm)))
	}

	s.Algorithm = algorithm
	s.KeyID = keyID
	s.TrustedComment = strings.TrimPrefix(trustedComment, "trusted comment: ")
	s.UntrustedComment = strings.TrimPrefix(untrustedComment, "untrusted comment: ")
	copy(s.Signature[:], rawSignature[10:])
	copy(s.CommentSignature[:], commentSignature)
	return nil
}

// encodeSignature encodes s into its textual representation.
func encodeSignature(s *Signature) []byte {
	var signature [2 + 8 + ed25519.SignatureSize]byte
	binary.LittleEndian.PutUint16(signature[:], s.Algorithm)
	binary.LittleEndian.PutUint64(signature[2:], s.KeyID)
	copy(signature[10:], s.Signature[:])

	b := make([]byte, 0, 228+len(s.TrustedComment)+len(s.UntrustedComment)) // Size of a signature in text format
	b = append(b, "untrusted comment: "...)
	b = append(b, s.UntrustedComment...)
	b = append(b, '\n')

	// TODO(aead): use base64.StdEncoding.EncodeAppend once Go1.21 is dropped
	n := len(b)
	b = b[:n+base64.StdEncoding.EncodedLen(len(signature))]
	base64.StdEncoding.Encode(b[n:], signature[:])
	b = append(b, '\n')

	b = append(b, "trusted comment: "...)
	b = append(b, s.TrustedComment...)
	b = append(b, '\n')

	// TODO(aead): use base64.StdEncoding.EncodeAppend once Go1.21 is dropped
	n = len(b)
	b = b[:n+base64.StdEncoding.EncodedLen(len(s.CommentSignature))]
	base64.StdEncoding.Encode(b[n:], s.CommentSignature[:])
	return append(b, '\n')
}
