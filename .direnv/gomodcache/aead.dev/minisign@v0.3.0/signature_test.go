// Copyright (c) 2021 Andreas Auernhammer. All rights reserved.
// Use of this source code is governed by a license that can be
// found in the LICENSE file.

package minisign

import (
	"strings"
	"testing"
)

func TestEqualSignature(t *testing.T) {
	for i, test := range equalSignatureTests {
		equal := test.A.Equal(test.B)
		if equal != test.Equal {
			t.Fatalf("Test %d: got 'equal=%v' - want 'equal=%v", i, equal, test.Equal)
		}
		if revEqual := test.B.Equal(test.A); equal != revEqual {
			t.Fatalf("Test %d: A == B is %v but B == A is %v", i, equal, revEqual)
		}
	}
}

func TestMarshalInvalidSignature(t *testing.T) {
	var signature Signature
	if _, err := signature.MarshalText(); err == nil {
		t.Fatal("Marshaling invalid signature succeeded")
	}
}

func TestMarshalSignatureRoundtrip(t *testing.T) {
	for i, test := range marshalSignatureTests {
		text, err := test.Signature.MarshalText()
		if err != nil {
			t.Fatalf("Test %d: failed to marshal signature: %v", i, err)
		}

		var signature Signature
		if err = signature.UnmarshalText(text); err != nil {
			t.Fatalf("Test %d: failed to unmarshal signature: %v", i, err)
		}

		if !signature.Equal(test.Signature) {
			t.Fatalf("Test %d: signature mismatch: got '%v' - want '%v'", i, signature, test.Signature)
		}
	}
}

func TestUnmarshalSignature(t *testing.T) {
	for i, test := range unmarshalSignatureTests {
		var signature Signature
		err := signature.UnmarshalText([]byte(test.Text))
		if err == nil && test.ShouldFail {
			t.Fatalf("Test %d: should have failed but passed", i)
		}
		if err != nil && !test.ShouldFail {
			t.Fatalf("Test %d: failed to unmarshal signature: %v", i, err)
		}
		if err == nil {
			if !signature.Equal(test.Signature) {
				t.Fatalf("Test %d: signatures are not equal: got '%s' - want '%s'", i, signature, test.Signature)
			}
			if signature.UntrustedComment != test.Signature.UntrustedComment {
				t.Fatalf("Test %d: untrusted comment mismatch: got '%s' - want '%s'", i, signature.UntrustedComment, test.Signature.UntrustedComment)
			}
		}
	}
}

func TestSignatureCarriageReturn(t *testing.T) {
	signature, err := SignatureFromFile("./internal/testdata/robtest.ps1.minisig")
	if err != nil {
		t.Fatalf("Failed to read signature from file: %v", err)
	}
	if strings.HasSuffix(signature.UntrustedComment, "\r") {
		t.Fatal("Untrusted comment ends with a carriage return")
	}
	if strings.HasSuffix(signature.TrustedComment, "\r") {
		t.Fatal("Trusted comment ends with a carriage return")
	}
}

var equalSignatureTests = []struct {
	A, B  Signature
	Equal bool
}{
	{
		A: Signature{}, B: Signature{}, Equal: true,
	},
	{
		A: Signature{
			Algorithm:        EdDSA,
			KeyID:            0xe7620f1842b4e81f,
			UntrustedComment: `signature from minisign secret key`,
			TrustedComment:   `timestamp:1591521248	file:minisign-0.9.tar.gz`,
			Signature:        [64]byte{20, 99, 118, 100, 132, 21, 202, 44, 47, 123, 240, 66, 228, 28, 175, 132, 143, 49, 11, 188, 252, 49, 53, 73, 106, 154, 66, 249, 67, 203, 35, 77, 156, 24, 226, 182, 244, 241, 252, 5, 244, 97, 127, 41, 191, 156, 128, 14, 117, 64, 157, 164, 36, 146, 238, 203, 151, 33, 174, 82, 239, 66, 73, 10},
			CommentSignature: [64]byte{148, 178, 205, 92, 217, 151, 10, 78, 112, 147, 154, 17, 47, 24, 233, 136, 141, 16, 37, 217, 29, 77, 64, 75, 217, 55, 69, 178, 114, 188, 40, 93, 6, 130, 93, 121, 211, 7, 19, 198, 190, 160, 33, 49, 136, 129, 80, 249, 121, 170, 165, 216, 105, 97, 230, 151, 208, 109, 244, 227, 46, 121, 241, 15},
		},
		B: Signature{
			Algorithm:        EdDSA,
			KeyID:            0xe7620f1842b4e81f,
			UntrustedComment: `signature from minisign secret key`,
			TrustedComment:   `timestamp:1591521248	file:minisign-0.9.tar.gz`,
			Signature:        [64]byte{20, 99, 118, 100, 132, 21, 202, 44, 47, 123, 240, 66, 228, 28, 175, 132, 143, 49, 11, 188, 252, 49, 53, 73, 106, 154, 66, 249, 67, 203, 35, 77, 156, 24, 226, 182, 244, 241, 252, 5, 244, 97, 127, 41, 191, 156, 128, 14, 117, 64, 157, 164, 36, 146, 238, 203, 151, 33, 174, 82, 239, 66, 73, 10},
			CommentSignature: [64]byte{148, 178, 205, 92, 217, 151, 10, 78, 112, 147, 154, 17, 47, 24, 233, 136, 141, 16, 37, 217, 29, 77, 64, 75, 217, 55, 69, 178, 114, 188, 40, 93, 6, 130, 93, 121, 211, 7, 19, 198, 190, 160, 33, 49, 136, 129, 80, 249, 121, 170, 165, 216, 105, 97, 230, 151, 208, 109, 244, 227, 46, 121, 241, 15},
		},
		Equal: true,
	},
	{
		A:     Signature{UntrustedComment: "signature A"},
		B:     Signature{UntrustedComment: "signature B"},
		Equal: true,
	},

	{
		A:     Signature{Algorithm: EdDSA},
		B:     Signature{Algorithm: HashEdDSA},
		Equal: false, // Algorithm differs
	},
	{
		A:     Signature{KeyID: 0xe7620f1842b4e81f},
		B:     Signature{KeyID: 0x1fe8b442180f62e7},
		Equal: false, // KeyID differs
	},
	{
		A:     Signature{TrustedComment: `timestamp:1591521248	file:minisign-0.9.tar.gz`},
		B:     Signature{TrustedComment: `timestamp:1591521249	file:minisign-0.9.tar.gz`},
		Equal: false, // TrustedComment differs
	},
	{
		A:     Signature{Signature: [64]byte{20, 99, 118, 100, 132, 21, 202, 44, 47, 123, 240, 66, 228, 28, 175, 132, 143, 49, 11, 188, 252, 49, 53, 73, 106, 154, 66, 249, 67, 203, 35, 77, 156, 24, 226, 182, 244, 241, 252, 5, 244, 97, 127, 41, 191, 156, 128, 14, 117, 64, 157, 164, 36, 146, 238, 203, 151, 33, 174, 82, 239, 66, 73, 10}},
		B:     Signature{Signature: [64]byte{148, 178, 205, 92, 217, 151, 10, 78, 112, 147, 154, 17, 47, 24, 233, 136, 141, 16, 37, 217, 29, 77, 64, 75, 217, 55, 69, 178, 114, 188, 40, 93, 6, 130, 93, 121, 211, 7, 19, 198, 190, 160, 33, 49, 136, 129, 80, 249, 121, 170, 165, 216, 105, 97, 230, 151, 208, 109, 244, 227, 46, 121, 241, 15}},
		Equal: false, // Signature differs
	},
	{
		A:     Signature{CommentSignature: [64]byte{20, 99, 118, 100, 132, 21, 202, 44, 47, 123, 240, 66, 228, 28, 175, 132, 143, 49, 11, 188, 252, 49, 53, 73, 106, 154, 66, 249, 67, 203, 35, 77, 156, 24, 226, 182, 244, 241, 252, 5, 244, 97, 127, 41, 191, 156, 128, 14, 117, 64, 157, 164, 36, 146, 238, 203, 151, 33, 174, 82, 239, 66, 73, 10}},
		B:     Signature{CommentSignature: [64]byte{148, 178, 205, 92, 217, 151, 10, 78, 112, 147, 154, 17, 47, 24, 233, 136, 141, 16, 37, 217, 29, 77, 64, 75, 217, 55, 69, 178, 114, 188, 40, 93, 6, 130, 93, 121, 211, 7, 19, 198, 190, 160, 33, 49, 136, 129, 80, 249, 121, 170, 165, 216, 105, 97, 230, 151, 208, 109, 244, 227, 46, 121, 241, 15}},
		Equal: false, // CommentSignature differs
	},
}

var marshalSignatureTests = []struct {
	Signature Signature
}{
	{
		Signature: Signature{
			Algorithm: EdDSA,
		},
	},
	{
		Signature: Signature{
			Algorithm: EdDSA,
			KeyID:     0xe7620f1842b4e81f,
		},
	},
	{
		Signature: Signature{
			Algorithm:        EdDSA,
			KeyID:            0xe7620f1842b4e81f,
			UntrustedComment: `signature from minisign secret key`,
		},
	},
	{
		Signature: Signature{
			Algorithm:        EdDSA,
			KeyID:            0xe7620f1842b4e81f,
			UntrustedComment: `signature from minisign secret key`,
			TrustedComment:   `timestamp:1591521248	file:minisign-0.9.tar.gz`,
		},
	},
	{
		Signature: Signature{
			Algorithm:        EdDSA,
			KeyID:            0xe7620f1842b4e81f,
			UntrustedComment: `signature from minisign secret key`,
			TrustedComment:   `timestamp:1591521248	file:minisign-0.9.tar.gz`,
			Signature:        [64]byte{20, 99, 118, 100, 132, 21, 202, 44, 47, 123, 240, 66, 228, 28, 175, 132, 143, 49, 11, 188, 252, 49, 53, 73, 106, 154, 66, 249, 67, 203, 35, 77, 156, 24, 226, 182, 244, 241, 252, 5, 244, 97, 127, 41, 191, 156, 128, 14, 117, 64, 157, 164, 36, 146, 238, 203, 151, 33, 174, 82, 239, 66, 73, 10},
		},
	},
	{
		Signature: Signature{
			Algorithm:        EdDSA,
			KeyID:            0xe7620f1842b4e81f,
			UntrustedComment: `signature from minisign secret key`,
			TrustedComment:   `timestamp:1591521248	file:minisign-0.9.tar.gz`,
			Signature:        [64]byte{20, 99, 118, 100, 132, 21, 202, 44, 47, 123, 240, 66, 228, 28, 175, 132, 143, 49, 11, 188, 252, 49, 53, 73, 106, 154, 66, 249, 67, 203, 35, 77, 156, 24, 226, 182, 244, 241, 252, 5, 244, 97, 127, 41, 191, 156, 128, 14, 117, 64, 157, 164, 36, 146, 238, 203, 151, 33, 174, 82, 239, 66, 73, 10},
			CommentSignature: [64]byte{148, 178, 205, 92, 217, 151, 10, 78, 112, 147, 154, 17, 47, 24, 233, 136, 141, 16, 37, 217, 29, 77, 64, 75, 217, 55, 69, 178, 114, 188, 40, 93, 6, 130, 93, 121, 211, 7, 19, 198, 190, 160, 33, 49, 136, 129, 80, 249, 121, 170, 165, 216, 105, 97, 230, 151, 208, 109, 244, 227, 46, 121, 241, 15},
		},
	},
}

var unmarshalSignatureTests = []struct {
	Text       string
	Signature  Signature
	ShouldFail bool
}{
	{
		Text: `untrusted comment: signature from minisign secret key
RWQf6LRCGA9i5xRjdmSEFcosL3vwQuQcr4SPMQu8/DE1SWqaQvlDyyNNnBjitvTx/AX0YX8pv5yADnVAnaQkku7LlyGuUu9CSQo=
trusted comment: timestamp:1591521248	file:minisign-0.9.tar.gz
lLLNXNmXCk5wk5oRLxjpiI0QJdkdTUBL2TdFsnK8KF0Ggl150wcTxr6gITGIgVD5eaql2Glh5pfQbfTjLnnxDw==`,
		Signature: Signature{
			Algorithm:        EdDSA,
			KeyID:            0xe7620f1842b4e81f,
			UntrustedComment: `signature from minisign secret key`,
			TrustedComment:   `timestamp:1591521248	file:minisign-0.9.tar.gz`,
			Signature:        [64]byte{20, 99, 118, 100, 132, 21, 202, 44, 47, 123, 240, 66, 228, 28, 175, 132, 143, 49, 11, 188, 252, 49, 53, 73, 106, 154, 66, 249, 67, 203, 35, 77, 156, 24, 226, 182, 244, 241, 252, 5, 244, 97, 127, 41, 191, 156, 128, 14, 117, 64, 157, 164, 36, 146, 238, 203, 151, 33, 174, 82, 239, 66, 73, 10},
			CommentSignature: [64]byte{148, 178, 205, 92, 217, 151, 10, 78, 112, 147, 154, 17, 47, 24, 233, 136, 141, 16, 37, 217, 29, 77, 64, 75, 217, 55, 69, 178, 114, 188, 40, 93, 6, 130, 93, 121, 211, 7, 19, 198, 190, 160, 33, 49, 136, 129, 80, 249, 121, 170, 165, 216, 105, 97, 230, 151, 208, 109, 244, 227, 46, 121, 241, 15},
		},
	},
	{
		Text: `untrusted comment: signature from minisign secret key
RWQf6LRCGA9i5xRjdmSEFcosL3vwQuQcr4SPMQu8/DE1SWqaQvlDyyNNnBjitvTx/AX0YX8pv5yADnVAnaQkku7LlyGuUu9CSQo=
trusted comment: timestamp:1591521248	file:minisign-0.9.tar.gz
lLLNXNmXCk5wk5oRLxjpiI0QJdkdTUBL2TdFsnK8KF0Ggl150wcTxr6gITGIgVD5eaql2Glh5pfQbfTjLnnxDw==` + "\n\r\n",
		Signature: Signature{
			Algorithm:        EdDSA,
			KeyID:            0xe7620f1842b4e81f,
			UntrustedComment: `signature from minisign secret key`,
			TrustedComment:   `timestamp:1591521248	file:minisign-0.9.tar.gz`,
			Signature:        [64]byte{20, 99, 118, 100, 132, 21, 202, 44, 47, 123, 240, 66, 228, 28, 175, 132, 143, 49, 11, 188, 252, 49, 53, 73, 106, 154, 66, 249, 67, 203, 35, 77, 156, 24, 226, 182, 244, 241, 252, 5, 244, 97, 127, 41, 191, 156, 128, 14, 117, 64, 157, 164, 36, 146, 238, 203, 151, 33, 174, 82, 239, 66, 73, 10},
			CommentSignature: [64]byte{148, 178, 205, 92, 217, 151, 10, 78, 112, 147, 154, 17, 47, 24, 233, 136, 141, 16, 37, 217, 29, 77, 64, 75, 217, 55, 69, 178, 114, 188, 40, 93, 6, 130, 93, 121, 211, 7, 19, 198, 190, 160, 33, 49, 136, 129, 80, 249, 121, 170, 165, 216, 105, 97, 230, 151, 208, 109, 244, 227, 46, 121, 241, 15},
		},
	},

	// Invalid signatures
	{
		Text: `RWQf6LRCGA9i5xRjdmSEFcosL3vwQuQcr4SPMQu8/DE1SWqaQvlDyyNNnBjitvTx/AX0YX8pv5yADnVAnaQkku7LlyGuUu9CSQo=
trusted comment: timestamp:1591521248	file:minisign-0.9.tar.gz
lLLNXNmXCk5wk5oRLxjpiI0QJdkdTUBL2TdFsnK8KF0Ggl150wcTxr6gITGIgVD5eaql2Glh5pfQbfTjLnnxDw==`,
		ShouldFail: true, // Missing untrusted comment
	},
	{
		Text: `untrusted: signature from minisign secret key
RWQf6LRCGA9i5xRjdmSEFcosL3vwQuQcr4SPMQu8/DE1SWqaQvlDyyNNnBjitvTx/AX0YX8pv5yADnVAnaQkku7LlyGuUu9CSQo=
trusted comment: timestamp:1591521248	file:minisign-0.9.tar.gz
lLLNXNmXCk5wk5oRLxjpiI0QJdkdTUBL2TdFsnK8KF0Ggl150wcTxr6gITGIgVD5eaql2Glh5pfQbfTjLnnxDw==`,
		ShouldFail: true, // Invalid untrusted comment - wrong prefix
	},

	{
		Text: `untrusted comment: signature from minisign secret key
trusted comment: timestamp:1591521248	file:minisign-0.9.tar.gz
lLLNXNmXCk5wk5oRLxjpiI0QJdkdTUBL2TdFsnK8KF0Ggl150wcTxr6gITGIgVD5eaql2Glh5pfQbfTjLnnxDw==`,
		ShouldFail: true, // Missing signature value
	},
	{
		Text: `untrusted comment: signature from minisign secret key
31TR+QBxE86BOJz1U46pc1lM1zEvMLBDTE255CHxFFLFcn4qPd3Q77xJTF2Y2IkDNqrTOCaZ43PQjSv9kIrnHXXwW0dwKnj
trusted comment: timestamp:1591521248	file:minisign-0.9.tar.gz
lLLNXNmXCk5wk5oRLxjpiI0QJdkdTUBL2TdFsnK8KF0Ggl150wcTxr6gITGIgVD5eaql2Glh5pfQbfTjLnnxDw==`,
		ShouldFail: true, // Invalid signature value - invalid base64
	},
	{
		Text: `untrusted comment: signature from minisign secret key
f4IYNY3p6K5CYtfB+dhN6Y+Fi+F6wWI0r+VjLwDE0q23wB1Opso6w/MJd9YGIU/HBs04flXnak37x/s2QhWAZlSCdbQYX7Q=
trusted comment: timestamp:1591521248	file:minisign-0.9.tar.gz
lLLNXNmXCk5wk5oRLxjpiI0QJdkdTUBL2TdFsnK8KF0Ggl150wcTxr6gITGIgVD5eaql2Glh5pfQbfTjLnnxDw==`,
		ShouldFail: true, // Invalid signature value - invalid size
	},

	{
		Text: `untrusted comment: signature from minisign secret key
RWQf6LRCGA9i5xRjdmSEFcosL3vwQuQcr4SPMQu8/DE1SWqaQvlDyyNNnBjitvTx/AX0YX8pv5yADnVAnaQkku7LlyGuUu9CSQo=
lLLNXNmXCk5wk5oRLxjpiI0QJdkdTUBL2TdFsnK8KF0Ggl150wcTxr6gITGIgVD5eaql2Glh5pfQbfTjLnnxDw==` + "\n\r\n",
		ShouldFail: true, // Missing trusted comment
	},
	{
		Text: `untrusted comment: signature from minisign secret key
RWQf6LRCGA9i5xRjdmSEFcosL3vwQuQcr4SPMQu8/DE1SWqaQvlDyyNNnBjitvTx/AX0YX8pv5yADnVAnaQkku7LlyGuUu9CSQo=
comment: timestamp:1591521248	file:minisign-0.9.tar.gz
lLLNXNmXCk5wk5oRLxjpiI0QJdkdTUBL2TdFsnK8KF0Ggl150wcTxr6gITGIgVD5eaql2Glh5pfQbfTjLnnxDw==` + "\n\r\n",
		ShouldFail: true, // Invalid trusted comment - wrong prefix
	},

	{
		Text: `untrusted comment: signature from minisign secret key
RWQf6LRCGA9i5xRjdmSEFcosL3vwQuQcr4SPMQu8/DE1SWqaQvlDyyNNnBjitvTx/AX0YX8pv5yADnVAnaQkku7LlyGuUu9CSQo=
trusted comment: timestamp:1591521248	file:minisign-0.9.tar.gz`,
		ShouldFail: true, // Missing comment signature
	},
	{
		Text: `untrusted comment: signature from minisign secret key
RWQf6LRCGA9i5xRjdmSEFcosL3vwQuQcr4SPMQu8/DE1SWqaQvlDyyNNnBjitvTx/AX0YX8pv5yADnVAnaQkku7LlyGuUu9CSQo=
trusted comment: timestamp:1591521248	file:minisign-0.9.tar.gz
Bqq219+sDloDkxHiCLcR5sTxrbl+qMS4oEnZ+IrZ4JDH5BxAzKehjoWSch3nbyNT96c/jz+XQjj4zd492skB_w==`,
		ShouldFail: true, // Invalid comment signature - invalid base64
	},
	{
		Text: `untrusted comment: signature from minisign secret key
RWQf6LRCGA9i5xRjdmSEFcosL3vwQuQcr4SPMQu8/DE1SWqaQvlDyyNNnBjitvTx/AX0YX8pv5yADnVAnaQkku7LlyGuUu9CSQo=
trusted comment: timestamp:1591521248	file:minisign-0.9.tar.gz
nqGtUS55Xhx/VzvCGtWjtsnlcItcsp0hzl/40j3oRkyJAISXHTakVQKK2VBBMyjBfhZTRRlEputvn/dNdC/Dh6Y=`,
		ShouldFail: true, // Invalid comment signature - invalid size
	},
}
