// Copyright (c) 2021 Andreas Auernhammer. All rights reserved.
// Use of this source code is governed by a license that can be
// found in the LICENSE file.

package minisign

import "testing"

var marshalPublicKeyTests = []struct {
	PublicKey PublicKey
	Text      string
}{
	{
		PublicKey: PublicKey{
			id:    0xe7620f1842b4e81f,
			bytes: [32]byte{121, 165, 97, 231, 14, 224, 140, 211, 231, 84, 198, 62, 155, 214, 185, 195, 82, 10, 29, 66, 4, 205, 16, 77, 162, 231, 239, 118, 59, 24, 83, 183},
		},
		Text: "untrusted comment: minisign public key: E7620F1842B4E81F" + "\n" + "RWQf6LRCGA9i53mlYecO4IzT51TGPpvWucNSCh1CBM0QTaLn73Y7GFO3",
	},
	{
		PublicKey: PublicKey{
			id:    0x6f7add142cdc7edb,
			bytes: [32]byte{121, 165, 97, 231, 14, 224, 140, 211, 231, 84, 198, 62, 155, 214, 185, 195, 82, 10, 29, 66, 4, 205, 16, 77, 162, 231, 239, 118, 59, 24, 83, 183},
		},
		Text: "untrusted comment: minisign public key: 6F7ADD142CDC7EDB" + "\n" + "RWTbftwsFN16b3mlYecO4IzT51TGPpvWucNSCh1CBM0QTaLn73Y7GFO3",
	},
}

func TestMarshalPublicKey(t *testing.T) {
	for i, test := range marshalPublicKeyTests {
		text, err := test.PublicKey.MarshalText()
		if err != nil {
			t.Fatalf("Test %d: failed to marshal public key: %v", i, err)
		}
		if string(text) != test.Text {
			t.Fatalf("Test %d: got '%s' - want '%s'", i, string(text), test.Text)
		}
	}
}

var unmarshalPublicKeyTests = []struct {
	Text       string
	PublicKey  PublicKey
	ShouldFail bool
}{
	{
		Text: "RWQf6LRCGA9i53mlYecO4IzT51TGPpvWucNSCh1CBM0QTaLn73Y7GFO3",
		PublicKey: PublicKey{
			id:    0xe7620f1842b4e81f,
			bytes: [32]byte{121, 165, 97, 231, 14, 224, 140, 211, 231, 84, 198, 62, 155, 214, 185, 195, 82, 10, 29, 66, 4, 205, 16, 77, 162, 231, 239, 118, 59, 24, 83, 183},
		},
	},
	{
		Text: "RWQf6LRCGA9i53mlYecO4IzT51TGPpvWucNSCh1CBM0QTaLn73Y7GFO3\r\n\n",
		PublicKey: PublicKey{
			id:    0xe7620f1842b4e81f,
			bytes: [32]byte{121, 165, 97, 231, 14, 224, 140, 211, 231, 84, 198, 62, 155, 214, 185, 195, 82, 10, 29, 66, 4, 205, 16, 77, 162, 231, 239, 118, 59, 24, 83, 183},
		},
	},
	{ // Invalid algorithm
		Text:       "RmQf6LRCGA9i53mlYecO4IzT51TGPpvWucNSCh1CBM0QTaLn73Y7GFO3",
		ShouldFail: true,
	},
	{ // Invalid public key b/c too long
		Text:       "RWQf6LRCGA9i53mlYecO4IzT51TGPpvWucNSCh1CBM0QTaLn73Y7GFO3bhQ=",
		ShouldFail: true,
	},
}

func TestUnmarshalPublicKey(t *testing.T) {
	for i, test := range unmarshalPublicKeyTests {
		var key PublicKey

		err := key.UnmarshalText([]byte(test.Text))
		if err == nil && test.ShouldFail {
			t.Fatalf("Test %d: should have failed but passed", i)
		}
		if err != nil && !test.ShouldFail {
			t.Fatalf("Test %d: failed to unmarshal public key: %v", i, err)
		}

		if err == nil {
			if key.ID() != test.PublicKey.ID() {
				t.Fatalf("Test %d: key ID mismatch: got '%x' - want '%x'", i, key.ID(), test.PublicKey.ID())
			}
			if key.bytes != test.PublicKey.bytes {
				t.Fatalf("Test %d: raw public key mismatch: got '%v' - want '%v'", i, key.bytes, test.PublicKey.bytes)
			}
			if !key.Equal(test.PublicKey) {
				t.Fatalf("Test %d: public keys are not equal", i)
			}
		}
	}
}
