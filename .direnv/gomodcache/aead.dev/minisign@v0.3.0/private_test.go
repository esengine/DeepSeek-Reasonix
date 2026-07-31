// Copyright (c) 2024 Andreas Auernhammer. All rights reserved.
// Use of this source code is governed by a license that can be
// found in the LICENSE file.

package minisign

import (
	"bytes"
	"encoding/base64"
	"os"
	"testing"
)

var marshalPrivateKeyTests = []struct {
	File  string
	ID    uint64
	Bytes []byte
}{
	{
		File:  "./internal/testdata/minisign-nopassword-0.key",
		ID:    0x3728470A8118E56E,
		Bytes: b64("JpjEI/XIKqIVl99tT611AxXwlVjlw2afJC8Nv6o7uuipyNvC3DmgO2csDT+bw1bZR3ss4rd5cXqoq0uftlCJqw=="),
	},
	{
		File:  "./internal/testdata/minisign-nopassword-1.key",
		ID:    0xD7E531EE76B2FC6F,
		Bytes: b64("L24Gi2UbWOb/MBb4MzJLysgC1F1FnE/m72qhb7r5FMlHzHe6M6mCLPMzmj6ln+hI51kqpDqTkIg9VCaToAhZtA=="),
	},
}

func TestPrivateKey_Marshal(t *testing.T) {
	for i, test := range marshalPrivateKeyTests {
		raw, err := os.ReadFile(test.File)
		if err != nil {
			t.Fatalf("Failed to read private key: %v", err)
		}
		raw = bytes.ReplaceAll(raw, []byte{'\r', '\n'}, []byte{'\n'})
		raw = bytes.TrimRight(raw, "\n")

		key := PrivateKey{
			id: test.ID,
		}
		copy(key.bytes[:], test.Bytes)

		text, err := key.MarshalText()
		if err != nil {
			t.Fatalf("Test %d: failed to marshal private key: %v", i, err)
		}
		if !bytes.Equal(text, raw) {
			t.Fatalf("Test %d: failed to marshal private key:\nGot: %v\nWant: %v\n", i, text, raw)
		}
	}
}

var unmarshalPrivateKeyTests = []struct {
	File  string
	ID    uint64
	Bytes []byte
}{
	{
		File:  "./internal/testdata/minisign-nopassword-0.key",
		ID:    0x3728470A8118E56E,
		Bytes: b64("JpjEI/XIKqIVl99tT611AxXwlVjlw2afJC8Nv6o7uuipyNvC3DmgO2csDT+bw1bZR3ss4rd5cXqoq0uftlCJqw=="),
	},
	{
		File:  "./internal/testdata/minisign-nopassword-1.key",
		ID:    0xD7E531EE76B2FC6F,
		Bytes: b64("L24Gi2UbWOb/MBb4MzJLysgC1F1FnE/m72qhb7r5FMlHzHe6M6mCLPMzmj6ln+hI51kqpDqTkIg9VCaToAhZtA=="),
	},
}

func TestPrivateKey_Unmarshal(t *testing.T) {
	for i, test := range unmarshalPrivateKeyTests {
		raw, err := os.ReadFile(test.File)
		if err != nil {
			t.Fatalf("Test %d: failed to read private key: %v", i, err)
		}

		var key PrivateKey
		if err := key.UnmarshalText(raw); err != nil {
			t.Fatalf("Test %d: failed to unmarshal private key: %v\nPrivate key:\n%s", i, err, string(raw))
		}

		// Print test vector for marshaling:
		// t.Logf("\n{\n\tID: htoi(\"%X\"),\n\tBytes: b64(\"%s\"),\n}", key.id, base64.StdEncoding.EncodeToString(key.bytes[:]))

		if key.ID() != test.ID {
			t.Fatalf("Test %d: ID mismatch: got '%x' - want '%x'", i, key.ID(), test.ID)
		}
		if !bytes.Equal(key.bytes[:], test.Bytes) {
			t.Fatalf("Test %d: private key mismatch: got '%x' - want '%x'", i, key.bytes, test.Bytes)
		}
	}
}

func b64(s string) []byte {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}
