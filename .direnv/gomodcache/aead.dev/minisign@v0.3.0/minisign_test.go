// Copyright (c) 2021 Andreas Auernhammer. All rights reserved.
// Use of this source code is governed by a license that can be
// found in the LICENSE file.

package minisign

import (
	"io"
	"os"
	"testing"
)

func TestRoundtrip(t *testing.T) {
	const Password = "correct horse battery staple"
	privateKey, err := PrivateKeyFromFile(Password, "./internal/testdata/minisign.key")
	if err != nil {
		t.Fatalf("Failed to load private key: %v", err)
	}

	message, err := os.ReadFile("./internal/testdata/message.txt")
	if err != nil {
		t.Fatalf("Failed to load message: %v", err)
	}
	signature := Sign(privateKey, message)

	publicKey, err := PublicKeyFromFile("./internal/testdata/minisign.pub")
	if err != nil {
		t.Fatalf("Failed to load public key: %v", err)
	}

	if !Verify(publicKey, message, signature) {
		t.Fatalf("Verification failed: signature %q - public key %q", signature, publicKey)
	}
}

func TestReaderRoundtrip(t *testing.T) {
	const Password = "correct horse battery staple"
	privateKey, err := PrivateKeyFromFile(Password, "./internal/testdata/minisign.key")
	if err != nil {
		t.Fatalf("Failed to load private key: %v", err)
	}

	file, err := os.Open("./internal/testdata/message.txt")
	if err != nil {
		t.Fatalf("Failed to open message: %v", err)
	}
	defer file.Close()

	reader := NewReader(file)
	if _, err = io.Copy(io.Discard, reader); err != nil {
		t.Fatalf("Failed to read message: %v", err)
	}
	signature := reader.Sign(privateKey)

	publicKey, err := PublicKeyFromFile("./internal/testdata/minisign.pub")
	if err != nil {
		t.Fatalf("Failed to load public key: %v", err)
	}
	if !reader.Verify(publicKey, signature) {
		t.Fatalf("Verification failed: signature %q - public key %q", signature, publicKey)
	}
}
