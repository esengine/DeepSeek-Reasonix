package selfupdate

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	"github.com/minio/selfupdate/internal/binarydist"
)

var (
	oldFile         = []byte{0xDE, 0xAD, 0xBE, 0xEF}
	newFile         = []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
	newFileChecksum = sha256.Sum256(newFile)
)

func cleanup(path string) {
	os.Remove(path)
	os.Remove(fmt.Sprintf(".%s.new", path))
}

// we write with a separate name for each test so that we can run them in parallel
func writeOldFile(path string, t *testing.T) {
	if err := ioutil.WriteFile(path, oldFile, 0777); err != nil {
		t.Fatalf("Failed to write file for testing preparation: %v", err)
	}
}

func validateUpdate(path string, err error, t *testing.T) {
	if err != nil {
		t.Fatalf("Failed to update: %v", err)
	}

	buf, err := ioutil.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read file post-update: %v", err)
	}

	if !bytes.Equal(buf, newFile) {
		t.Fatalf("File was not updated! Bytes read: %v, Bytes expected: %v", buf, newFile)
	}
}

func TestApplySimple(t *testing.T) {
	fName := "TestApplySimple"
	defer cleanup(fName)
	writeOldFile(fName, t)

	err := Apply(bytes.NewReader(newFile), Options{
		TargetPath: fName,
	})
	validateUpdate(fName, err, t)
}

func TestApplyOldSavePath(t *testing.T) {
	fName := "TestApplyOldSavePath"
	defer cleanup(fName)
	writeOldFile(fName, t)

	oldfName := "OldSavePath"

	err := Apply(bytes.NewReader(newFile), Options{
		TargetPath:  fName,
		OldSavePath: oldfName,
	})
	validateUpdate(fName, err, t)

	if _, err := os.Stat(oldfName); os.IsNotExist(err) {
		t.Fatalf("Failed to find the old file: %v", err)
	}

	cleanup(oldfName)
}

func TestVerifyChecksum(t *testing.T) {
	fName := "TestVerifyChecksum"
	defer cleanup(fName)
	writeOldFile(fName, t)

	err := Apply(bytes.NewReader(newFile), Options{
		TargetPath: fName,
		Checksum:   newFileChecksum[:],
	})
	validateUpdate(fName, err, t)
}

func TestVerifyChecksumNegative(t *testing.T) {
	fName := "TestVerifyChecksumNegative"
	defer cleanup(fName)
	writeOldFile(fName, t)

	badChecksum := []byte{0x0A, 0x0B, 0x0C, 0xFF}
	err := Apply(bytes.NewReader(newFile), Options{
		TargetPath: fName,
		Checksum:   badChecksum,
	})
	if err == nil {
		t.Fatalf("Failed to detect bad checksum!")
	}
}

func TestApplyPatch(t *testing.T) {
	fName := "TestApplyPatch"
	defer cleanup(fName)
	writeOldFile(fName, t)

	patch := new(bytes.Buffer)
	err := binarydist.Diff(bytes.NewReader(oldFile), bytes.NewReader(newFile), patch)
	if err != nil {
		t.Fatalf("Failed to create patch: %v", err)
	}

	err = Apply(patch, Options{
		TargetPath: fName,
		Patcher:    NewBSDiffPatcher(),
	})
	validateUpdate(fName, err, t)
}

func TestCorruptPatch(t *testing.T) {
	fName := "TestCorruptPatch"
	defer cleanup(fName)
	writeOldFile(fName, t)

	badPatch := []byte{0x44, 0x38, 0x86, 0x3c, 0x4f, 0x8d, 0x26, 0x54, 0xb, 0x11, 0xce, 0xfe, 0xc1, 0xc0, 0xf8, 0x31, 0x38, 0xa0, 0x12, 0x1a, 0xa2, 0x57, 0x2a, 0xe1, 0x3a, 0x48, 0x62, 0x40, 0x2b, 0x81, 0x12, 0xb1, 0x21, 0xa5, 0x16, 0xed, 0x73, 0xd6, 0x54, 0x84, 0x29, 0xa6, 0xd6, 0xb2, 0x1b, 0xfb, 0xe6, 0xbe, 0x7b, 0x70}
	err := Apply(bytes.NewReader(badPatch), Options{
		TargetPath: fName,
		Patcher:    NewBSDiffPatcher(),
	})
	if err == nil {
		t.Fatalf("Failed to detect corrupt patch!")
	}
}

func TestVerifyChecksumPatchNegative(t *testing.T) {
	fName := "TestVerifyChecksumPatchNegative"
	defer cleanup(fName)
	writeOldFile(fName, t)

	patch := new(bytes.Buffer)
	anotherFile := []byte{0x77, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66}
	err := binarydist.Diff(bytes.NewReader(oldFile), bytes.NewReader(anotherFile), patch)
	if err != nil {
		t.Fatalf("Failed to create patch: %v", err)
	}

	err = Apply(patch, Options{
		TargetPath: fName,
		Checksum:   newFileChecksum[:],
		Patcher:    NewBSDiffPatcher(),
	})
	if err == nil {
		t.Fatalf("Failed to detect patch to wrong file!")
	}
}

const ecdsaPublicKey = `
-----BEGIN PUBLIC KEY-----
MHYwEAYHKoZIzj0CAQYFK4EEACIDYgAEL8ThbSyEucsCxnd4dCZR2hIy5nea54ko
O+jUUfIjkvwhCWzASm0lpCVdVpXKZXIe+NZ+44RQRv3+OqJkCCGzUgJkPNI3lxdG
9zu8rbrnxISV06VQ8No7Ei9wiTpqmTBB
-----END PUBLIC KEY-----
`

const ecdsaPrivateKey = `
-----BEGIN EC PRIVATE KEY-----
MIGkAgEBBDBttCB/1NOY4T+WrG4FSV49Ayn3gK1DNzfGaJ01JUXeiNFCWQM2pqpU
om8ATPP/dkegBwYFK4EEACKhZANiAAQvxOFtLIS5ywLGd3h0JlHaEjLmd5rniSg7
6NRR8iOS/CEJbMBKbSWkJV1Wlcplch741n7jhFBG/f46omQIIbNSAmQ80jeXF0b3
O7ytuufEhJXTpVDw2jsSL3CJOmqZMEE=
-----END EC PRIVATE KEY-----
`

const rsaPublicKey = `
-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAxSWmu7trWKAwDFjiCN2D
Tk2jj2sgcr/CMlI4cSSiIOHrXCFxP1I8i9PvQkd4hasXQrLbT5WXKrRGv1HKUKab
b9ead+kD0kxk7i2bFYvKX43oq66IW0mOLTQBO7I9UyT4L7svcMD+HUQ2BqHoaQe4
y20C59dPr9Dpcz8DZkdLsBV6YKF6Ieb3iGk8oRLMWNaUqPa8f1BGgxAkvPHcqDjT
x4xRnjgTRRRlZvRtALHMUkIChgxDOhoEzKpGiqnX7HtMJfrhV6h0PAXNA4h9Kjv5
5fhJ08Rz7mmZmtH5JxTK5XTquo59sihSajR4bSjZbbkQ1uLkeFlY3eli3xdQ7Nrf
fQIDAQAB
-----END PUBLIC KEY-----`

const rsaPrivateKey = `
-----BEGIN RSA PRIVATE KEY-----
MIIEogIBAAKCAQEAxSWmu7trWKAwDFjiCN2DTk2jj2sgcr/CMlI4cSSiIOHrXCFx
P1I8i9PvQkd4hasXQrLbT5WXKrRGv1HKUKabb9ead+kD0kxk7i2bFYvKX43oq66I
W0mOLTQBO7I9UyT4L7svcMD+HUQ2BqHoaQe4y20C59dPr9Dpcz8DZkdLsBV6YKF6
Ieb3iGk8oRLMWNaUqPa8f1BGgxAkvPHcqDjTx4xRnjgTRRRlZvRtALHMUkIChgxD
OhoEzKpGiqnX7HtMJfrhV6h0PAXNA4h9Kjv55fhJ08Rz7mmZmtH5JxTK5XTquo59
sihSajR4bSjZbbkQ1uLkeFlY3eli3xdQ7NrffQIDAQABAoIBAAkN+6RvrTR61voa
Mvd5RQiZpEN4Bht/Fyo8gH8h0Zh1B9xJZOwlmMZLS5fdtHlfLEhR8qSrGDBL61vq
I8KkhEsUufF78EL+YzxVN+Q7cWYGHIOWFokqza7hzpSxUQO6lPOMQ1eIZaNueJTB
Zu07/47ISPPg/bXzgGVcpYlTCPTjUwKjtfyMqvX9AD7fIyYRm6zfE7EHj1J2sBFt
Yz1OGELg6HfJwXfpnPfBvftD0hWGzJ78Bp71fPJe6n5gnqmSqRvrcXNWFnH/yqkN
d6vPIxD6Z3LjvyZpkA7JillLva2L/zcIFhg4HZvQnWd8/PpDnUDonu36hcj4SC5j
W4aVPLkCgYEA4XzNKWxqYcajzFGZeSxlRHupSAl2MT7Cc5085MmE7dd31wK2T8O4
n7N4bkm/rjTbX85NsfWdKtWb6mpp8W3VlLP0rp4a/12OicVOkg4pv9LZDmY0sRlE
YuDJk1FeCZ50UrwTZI3rZ9IhZHhkgVA6uWAs7tYndONkxNHG0pjqs4sCgYEA39MZ
JwMqo3qsPntpgP940cCLflEsjS9hYNO3+Sv8Dq3P0HLVhBYajJnotf8VuU0fsQZG
grmtVn1yThFbMq7X1oY4F0XBA+paSiU18c4YyUnwax2u4sw9U/Q9tmQUZad5+ueT
qriMBwGv+ewO+nQxqvAsMUmemrVzrfwA5Oct+hcCgYAfiyXoNZJsOy2O15twqBVC
j0oPGcO+/9iT89sg5lACNbI+EdMPNYIOVTzzsL1v0VUfAe08h++Enn1BPcG0VHkc
ZFBGXTfJoXzfKQrkw7ZzbzuOGB4m6DH44xlP0oIlNlVvfX/5ASF9VJf3RiBJNsAA
TsP6ZVr/rw/ZuL7nlxy+IQKBgDhL/HOXlE3yOQiuOec8WsNHTs7C1BXe6PtVxVxi
988pYK/pclL6zEq5G5NLSceF4obAMVQIJ9UtUGbabrncyGUo9UrFPLsjYvprSZo8
YHegpVwL50UcYgCP2kXZ/ldjPIcjYDz8lhvdDMor2cidGTEJn9P11HLNWP9V91Ob
4jCZAoGAPNRSC5cC8iP/9j+s2/kdkfWJiNaolPYAUrmrkL6H39PYYZM5tnhaIYJV
Oh9AgABamU0eb3p3vXTISClVgV7ifq1HyZ7BSUhMfaY2Jk/s3sUHCWFxPZe9sgEG
KinIY/373KIkIV/5g4h2v1w330IWcfptxKcY/Er3DJr38f695GE=
-----END RSA PRIVATE KEY-----`

func signec(privatePEM string, source []byte, t *testing.T) []byte {
	parseFn := func(p []byte) (crypto.Signer, error) { return x509.ParseECPrivateKey(p) }
	return sign(parseFn, privatePEM, source, t)
}

func signrsa(privatePEM string, source []byte, t *testing.T) []byte {
	parseFn := func(p []byte) (crypto.Signer, error) { return x509.ParsePKCS1PrivateKey(p) }
	return sign(parseFn, privatePEM, source, t)
}

func sign(parsePrivKey func([]byte) (crypto.Signer, error), privatePEM string, source []byte, t *testing.T) []byte {
	block, _ := pem.Decode([]byte(privatePEM))
	if block == nil {
		t.Fatalf("Failed to parse private key PEM")
	}

	priv, err := parsePrivKey(block.Bytes)
	if err != nil {
		t.Fatalf("Failed to parse private key DER: %v", err)
	}

	checksum := sha256.Sum256(source)
	sig, err := priv.Sign(rand.Reader, checksum[:], crypto.SHA256)
	if err != nil {
		t.Fatalf("Failed to sign: %v", sig)
	}

	return sig
}
