package selfupdate

import (
	"errors"
	"io"
	"net/http"

	"aead.dev/minisign"
)

type Verifier struct {
	publicKey minisign.PublicKey
	signature minisign.Signature
}

func (v *Verifier) LoadFromURL(signatureURL string, passphrase string, transport http.RoundTripper) error {
	var publicKey minisign.PublicKey
	if err := publicKey.UnmarshalText([]byte(passphrase)); err != nil {
		return err
	}

	client := &http.Client{Transport: transport}
	req, err := http.NewRequest(http.MethodGet, signatureURL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New(resp.Status)
	}

	const MaxSize = 1 << 20
	b, err := io.ReadAll(io.LimitReader(resp.Body, MaxSize))
	if err != nil {
		return err
	}
	var signature minisign.Signature
	if err = signature.UnmarshalText(b); err != nil {
		return err
	}
	v.publicKey, v.signature = publicKey, signature
	return nil
}

func (v *Verifier) LoadFromFile(signaturePath string, passphrase string) error {
	var publicKey minisign.PublicKey
	if err := publicKey.UnmarshalText([]byte(passphrase)); err != nil {
		return err
	}
	signature, err := minisign.SignatureFromFile(signaturePath)
	if err != nil {
		return err
	}
	v.publicKey, v.signature = publicKey, signature
	return nil
}

func NewVerifier() *Verifier {
	return &Verifier{}
}

func (v *Verifier) Verify(bin []byte) error {
	signature, err := v.signature.MarshalText()
	if err != nil {
		return err
	}
	if !minisign.Verify(v.publicKey, bin, signature) {
		return errors.New("selfupdate: signature verification failed")
	}
	return nil
}
