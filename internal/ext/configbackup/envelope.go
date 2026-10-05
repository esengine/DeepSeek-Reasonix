package configbackup

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

// MaxEnvelopeBytes is the largest sealed backup this build produces or reads.
// The accounts service enforces the same bound on what it stores.
const MaxEnvelopeBytes = 4 << 20

// MinPassphraseRunes is the shortest passphrase Seal accepts. Argon2id slows a
// guess down; it cannot make a four-letter word safe.
const MinPassphraseRunes = 10

var envelopeMagic = []byte("RXCB")

const envelopeVersion = 1

// RFC 9106's second recommended profile: 64 MiB, three passes. Open accepts a
// bounded range so a later build may raise them without orphaning old backups,
// while a hostile header cannot make the reader allocate gigabytes.
const (
	kdfTime    = 3
	kdfMemory  = 64 * 1024
	kdfThreads = 4
	saltBytes  = 16
)

var (
	ErrWeakPassphrase = errors.New("configbackup: passphrase is too short")
	// ErrCannotDecrypt covers a wrong passphrase and a tampered envelope alike:
	// an AEAD cannot tell them apart, and guessing would mislead.
	ErrCannotDecrypt = errors.New("configbackup: wrong passphrase or damaged backup")
)

type envelopeHeader struct {
	KDF     string `json:"kdf"`
	Time    uint32 `json:"t"`
	Memory  uint32 `json:"m"`
	Threads uint8  `json:"p"`
	Salt    []byte `json:"salt"`
	AEAD    string `json:"aead"`
	Nonce   []byte `json:"nonce"`
}

// Seal encodes and encrypts a snapshot under passphrase.
func Seal(s *Snapshot, passphrase string) ([]byte, error) {
	if utf8.RuneCountInString(passphrase) < MinPassphraseRunes {
		return nil, ErrWeakPassphrase
	}
	plain, err := encode(s)
	if err != nil {
		return nil, err
	}
	h := envelopeHeader{
		KDF: "argon2id", Time: kdfTime, Memory: kdfMemory, Threads: kdfThreads,
		Salt: make([]byte, saltBytes), AEAD: "xchacha20poly1305", Nonce: make([]byte, chacha20poly1305.NonceSizeX),
	}
	if _, err := rand.Read(h.Salt); err != nil {
		return nil, err
	}
	if _, err := rand.Read(h.Nonce); err != nil {
		return nil, err
	}
	prefix, err := envelopePrefix(h)
	if err != nil {
		return nil, err
	}
	aead, err := chacha20poly1305.NewX(deriveKey(passphrase, h))
	if err != nil {
		return nil, err
	}
	out := aead.Seal(prefix, h.Nonce, plain, prefix)
	if len(out) > MaxEnvelopeBytes {
		return nil, ErrTooLarge
	}
	return out, nil
}

// Open authenticates and decrypts an envelope and decodes its snapshot.
func Open(envelope []byte, passphrase string) (*Snapshot, error) {
	if len(envelope) > MaxEnvelopeBytes {
		return nil, ErrTooLarge
	}
	h, prefixLen, err := parseHeader(envelope)
	if err != nil {
		return nil, err
	}
	aead, err := chacha20poly1305.NewX(deriveKey(passphrase, h))
	if err != nil {
		return nil, err
	}
	prefix := envelope[:prefixLen]
	plain, err := aead.Open(nil, h.Nonce, envelope[prefixLen:], prefix)
	if err != nil {
		return nil, ErrCannotDecrypt
	}
	return decode(plain)
}

func deriveKey(passphrase string, h envelopeHeader) []byte {
	return argon2.IDKey([]byte(passphrase), h.Salt, h.Time, h.Memory, h.Threads, chacha20poly1305.KeySize)
}

func envelopePrefix(h envelopeHeader) ([]byte, error) {
	header, err := json.Marshal(h)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.Write(envelopeMagic)
	buf.WriteByte(envelopeVersion)
	_ = binary.Write(&buf, binary.BigEndian, uint16(len(header)))
	buf.Write(header)
	return buf.Bytes(), nil
}

func parseHeader(envelope []byte) (envelopeHeader, int, error) {
	var h envelopeHeader
	fixed := len(envelopeMagic) + 3
	if len(envelope) < fixed || !bytes.Equal(envelope[:len(envelopeMagic)], envelopeMagic) {
		return h, 0, fmt.Errorf("%w: not a Reasonix backup", ErrMalformed)
	}
	if envelope[len(envelopeMagic)] != envelopeVersion {
		return h, 0, ErrUnsupportedFormat
	}
	n := int(binary.BigEndian.Uint16(envelope[len(envelopeMagic)+1 : fixed]))
	if len(envelope) < fixed+n {
		return h, 0, fmt.Errorf("%w: truncated header", ErrMalformed)
	}
	if err := json.Unmarshal(envelope[fixed:fixed+n], &h); err != nil {
		return h, 0, fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	if h.KDF != "argon2id" || h.AEAD != "xchacha20poly1305" {
		return h, 0, ErrUnsupportedFormat
	}
	if h.Time < 1 || h.Time > 10 || h.Memory < 8*1024 || h.Memory > 256*1024 || h.Threads < 1 || h.Threads > 16 ||
		len(h.Salt) < saltBytes || len(h.Salt) > 64 || len(h.Nonce) != chacha20poly1305.NonceSizeX {
		return h, 0, fmt.Errorf("%w: parameters out of range", ErrMalformed)
	}
	return h, fixed + n, nil
}
