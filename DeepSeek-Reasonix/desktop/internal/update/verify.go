package update

import (
	"errors"
	"fmt"

	"aead.dev/minisign"
)

// publicKey is the minisign public key that desktop release artifacts are signed
// with. The public half is safe to embed; the private half lives only in CI
// secrets (generated with `cmd/sign genkey`). Key ID AF12CA46F4A9EBB0. If the
// signing key is ever rotated, regenerate and update this constant in lockstep
// with the CI secret.
const publicKey = `untrusted comment: minisign public key: AF12CA46F4A9EBB0
RWSw66n0RsoSr6Zhh6qt5YO95YkpCayTOCMFVDNUQSjJYwxoYngNVBSq`

// Verify reports whether sig (the contents of a .minisig file) is a valid minisign
// signature of data under the embedded public key. A nil return means the artifact
// is authentic; any error means do not trust it. Callers MUST verify before
// touching disk — never apply an update whose signature has not checked out.
func Verify(data, sig []byte) error { return verifyWith(publicKey, data, sig) }

// PublicKey returns the embedded public key in its canonical two-line text form,
// so docs/UI can surface it for manual `minisign -Vm <file>` verification.
func PublicKey() string { return publicKey }

// verifyWith is the testable core: it parses an arbitrary public-key text and
// verifies the signature, letting tests use a throwaway key pair without the
// embedded key's (secret) counterpart.
func verifyWith(pubText string, data, sig []byte) error {
	var key minisign.PublicKey
	if err := key.UnmarshalText([]byte(pubText)); err != nil {
		return fmt.Errorf("update: parse public key: %w", err)
	}
	if !minisign.Verify(key, data, sig) {
		return errors.New("update: signature verification failed")
	}
	return nil
}
