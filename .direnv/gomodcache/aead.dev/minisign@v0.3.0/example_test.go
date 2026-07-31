// Copyright (c) 2021 Andreas Auernhammer. All rights reserved.
// Use of this source code is governed by a license that can be
// found in the LICENSE file.

package minisign_test

import (
	"crypto/rand"
	"fmt"
	"io"
	"strconv"
	"strings"

	"aead.dev/minisign"
)

func ExampleGenerateKey() {
	// Generate a new minisign private / public key pair.
	publicKey, privateKey, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		panic(err) // TODO: error handling
	}

	// Sign a message with the private key
	message := []byte("Hello Gopher!")
	signature := minisign.Sign(privateKey, message)

	// Verify the signature with the public key and
	// print the message if the signature is valid.
	if minisign.Verify(publicKey, message, signature) {
		fmt.Println(string(message))
	}
	// Output: Hello Gopher!
}

func ExampleEncryptKey() {
	// Generate a new minisign private / public key pair.
	// We don't care about the public key in this example.
	_, privateKey, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		panic(err) // TODO: error handling
	}

	const password = "correct horse battery staple"

	// Encrypt the private key with the password
	encryptedKey, err := minisign.EncryptKey(password, privateKey)
	if err != nil {
		panic(err) // TODO: error handling
	}

	// Then, decrypt the encrypted key with the password again
	decryptedKey, err := minisign.DecryptKey(password, encryptedKey)
	if err != nil {
		panic(err) // TODO: error handling
	}

	// Now, both private keys should be identical
	fmt.Println(privateKey.Equal(decryptedKey))
	// Output: true
}

func ExampleDecryptKey() {
	const (
		rawPrivateKey = "RWRTY0IyorAWr/1gdweGki6ua7GpmoPqS+7rMBSmBy6hedA53dAAABAAAAAAAAAAAAIAAAAAwfmyB6qIIW2eGNiQaFzgs1oi52iN8cRHBPRupc9TVdfAeJvlPdvzu3TfA2DHTW2PZi98uihcr5sEB5fefFml2d0xBk72ZOGNJpOTsn95eHgEH/qUfzQZ018JfiVwWf8pNpdgNFX8ROs="
		password      = "correct horse battery staple"
	)

	// Decrypt the raw private key with the password
	privateKey, err := minisign.DecryptKey(password, []byte(rawPrivateKey))
	if err != nil {
		panic(err) // TODO: error handling
	}

	// Print the key ID as upper-case hex string
	fmt.Println("Private Key", strings.ToUpper(strconv.FormatUint(privateKey.ID(), 16)))

	// Output: Private Key A345BDA18A33D06
}

func ExampleSign() {
	const (
		rawPrivateKey = "RWRTY0IyorAWr/1gdweGki6ua7GpmoPqS+7rMBSmBy6hedA53dAAABAAAAAAAAAAAAIAAAAAwfmyB6qIIW2eGNiQaFzgs1oi52iN8cRHBPRupc9TVdfAeJvlPdvzu3TfA2DHTW2PZi98uihcr5sEB5fefFml2d0xBk72ZOGNJpOTsn95eHgEH/qUfzQZ018JfiVwWf8pNpdgNFX8ROs="
		password      = "correct horse battery staple"
	)

	// Decrypt the raw private key with the password
	privateKey, err := minisign.DecryptKey(password, []byte(rawPrivateKey))
	if err != nil {
		panic(err) // TODO: error handling
	}

	// Sign a message with the private key
	message := []byte("Hello Gopher!")
	signature := minisign.Sign(privateKey, message)

	fmt.Println(string(signature))
}

func ExampleSignWithComments() {
	const (
		rawPrivateKey = "RWRTY0IyorAWr/1gdweGki6ua7GpmoPqS+7rMBSmBy6hedA53dAAABAAAAAAAAAAAAIAAAAAwfmyB6qIIW2eGNiQaFzgs1oi52iN8cRHBPRupc9TVdfAeJvlPdvzu3TfA2DHTW2PZi98uihcr5sEB5fefFml2d0xBk72ZOGNJpOTsn95eHgEH/qUfzQZ018JfiVwWf8pNpdgNFX8ROs="
		password      = "correct horse battery staple"
	)

	// Decrypt the raw private key with the password
	privateKey, err := minisign.DecryptKey(password, []byte(rawPrivateKey))
	if err != nil {
		panic(err) // TODO: error handling
	}

	// Sign a message with comments with the private key
	const (
		trustedComment   = "This comment is signed and can be trusted"
		untrustedComment = "This comment is not signed and just informational"
	)
	message := []byte("Hello Gopher!")
	signature := minisign.SignWithComments(privateKey, message, trustedComment, untrustedComment)

	fmt.Println(string(signature))

	// Output: untrusted comment: This comment is not signed and just informational
	// RWQGPaMY2ls0CmMflCAP5J/MpaXmt+3+UoT1vRSPRjXO6w0KNtpkcQe3TxQ35kAwhjFVB6CEYYrHZmMvWjXRutefRHicRUiAJwQ=
	// trusted comment: This comment is signed and can be trusted
	// /jXXGSI/q3MhrZ5PKzL221/qC+JFVpgilf9su6AcTtMffw+9ShYt5LjU2RG1M/EspIoEv4xxK/36TeCQBgHbBw==
}

func ExampleVerify() {
	const rawPublicKey = "RWQGPaMY2ls0CkF/83ls7D+IU25w3jeYczwo3s451zDlnrJJwOdt2ro8"

	var (
		message = []byte("Hello Gopher!")

		signature = []byte(`untrusted comment: signature from private key: A345BDA18A33D06
RWQGPaMY2ls0CmMflCAP5J/MpaXmt+3+UoT1vRSPRjXO6w0KNtpkcQe3TxQ35kAwhjFVB6CEYYrHZmMvWjXRutefRHicRUiAJwQ=
trusted comment: timestamp:1600100266
2x/lxCqL+PHoT4I9Wc8PHmoNBtohgmFdWwPBON55Y2P0ttpBHgr4OFldr/Hq7nDcBGt5SBs2XjtMnxjVs6byBg==`)
	)

	var publicKey minisign.PublicKey
	if err := publicKey.UnmarshalText([]byte(rawPublicKey)); err != nil {
		panic(err) // TODO: error handling
	}

	if minisign.Verify(publicKey, message, signature) {
		fmt.Println(string(message))
	}
	// Output: Hello Gopher!
}

func ExampleReader() {
	// Generate a new minisign public / private key pair.
	publicKey, privateKey, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		panic(err) // TODO: error handling
	}

	const Message = "Hello Gopher!"

	// Sign a data stream after processing it. (Here, we just discard it)
	reader := minisign.NewReader(strings.NewReader(Message))
	if _, err := io.Copy(io.Discard, reader); err != nil {
		panic(err) // TODO: error handling
	}
	signature := reader.Sign(privateKey)

	// Read a data stream and then verify its authenticity with
	// the public key.
	reader = minisign.NewReader(strings.NewReader(Message))
	message, err := io.ReadAll(reader)
	if err != nil {
		panic(err) // TODO: error handling
	}
	if reader.Verify(publicKey, signature) {
		fmt.Println(string(message))
	}
	// Output: Hello Gopher!
}
