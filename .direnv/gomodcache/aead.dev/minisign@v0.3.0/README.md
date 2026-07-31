[![Go Reference](https://pkg.go.dev/badge/aead.dev/minisign.svg)](https://pkg.go.dev/aead.dev/minisign)
![Github CI](https://github.com/aead/minisign/actions/workflows/go.yml/badge.svg?branch=main)
[![latest](https://badgen.net/github/tag/aead/minisign)](https://github.com/aead/minisign/releases/latest)

# minisign

minisign is a dead simple tool to sign files and verify signatures.

```
$ minisign -G                                                                                  
Please enter a password to protect the secret key.

Enter Password: 
Enter Password (one more time): 
Deriving a key from the password in order to encrypt the secret key... done

The secret key was saved as ~/.minisign/minisign.key - Keep it secret!
The public key was saved as minisign.pub - That one can be public.

Files signed using this key pair can be verified with the following command:

minisign -Vm <file> -P RWSYKA736yqh+JrZ7cRDdWgck/WKtwW9ATBFmk8pQ1lHeUKXtV6uJ7Fu
```
```
$ minisign -Sm message.txt
Enter password: 
Deriving a key from the password in order to decrypt the secret key... done
```
```
$ minisign -Vm message.txt
Signature and comment signature verified
Trusted comment: timestamp:1614718943	filename:message.txt
```

This is a Go implementation of the [original C implementation](https://github.com/jedisct1/minisign) by [Frank Denis](https://github.com/jedisct1).

## Usage

```
Usage:
    minisign -G [-p <pubKey>] [-s <secKey>] [-W]
    minisign -R [-s <secKey>] [-p <pubKey>]
    minisign -C [-s <secKey>] [-W]
    minisign -S [-x <signature>] [-s <secKey>] [-c <comment>] [-t <comment>] -m <file>...
    minisign -V [-H] [-x <signature>] [-p <pubKey> | -P <pubKey>] [-o] [-q | -Q ] -m <file>

Options:
    -G               Generate a new public/secret key pair.
    -R               Re-create a public key file from a secret key.
    -C               Change or remove the password of the secret key.
    -S               Sign files with a secret key.
    -V               Verify files with a public key.
    -m <file>        The file to sign or verify.
    -o               Combined with -V, output the file after verification.
    -H               Combined with -V, require a signature over a pre-hashed file.
    -p <pubKey>      Public key file (default: ./minisign.pub)
    -P <pubKey>      Public key as base64 string
    -s <secKey>      Secret key file (default: $HOME/.minisign/minisign.key)
    -W               Do not encrypt/decrypt the secret key with a password.
    -x <signature>   Signature file (default: <file>.minisig)
    -c <comment>     Add a one-line untrusted comment.
    -t <comment>     Add a one-line trusted comment.
    -q               Quiet mode. Suppress output.
    -Q               Pretty quiet mode. Combined with -V, only print the trusted comment.
    -f               Combined with -G or -R, overwrite any existing public/secret key pair.
    -v               Print version information.
```

## Installation

With an up-to-date Go toolchain:
```
go install aead.dev/minisign/cmd/minisign@latest
```

On windows, linux and macOS, you can also use the pre-built binaries:
| OS        | ARCH    | Latest Release                                                                                                         |
|:---------:|:-------:|:-----------------------------------------------------------------------------------------------------------------------|
| Linux     | amd64   | [minisign-linux-amd64.tar.gz](https://github.com/aead/minisign/releases/download/v0.3.0/minisign-linux-amd64.tar.gz)   |
| Linux     | arm64   | [minisign-linux-arm64.tar.gz](https://github.com/aead/minisign/releases/download/v0.3.0/minisign-linux-arm64.tar.gz)   |
| MacOS     | arm64   | [minisign-darwin-arm64.tar.gz](https://github.com/aead/minisign/releases/download/v0.3.0/minisign-darwin-arm64.tar.gz) |
| Windows   | amd64   | [minisign-windows-amd64.zip](https://github.com/aead/minisign/releases/download/v0.3.0/minisign-windows-amd64.zip)     |

From source:
1. Clone the repository
   ```
   git clone https://aead.dev/minisign && cd minisign
   ```
2. Build the binary
   ```
   make build
   ```

## Library

```Go
import "aead.dev/minisign" 
```

The following example generates a minisign public/private key pair, signs a message and verifies the message signature.

```Go
package main

import (
	"crypto/rand"
	"log"

	"aead.dev/minisign"
)

func main() {
	var message = []byte("Hello World!")

	publicKey, privateKey, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		log.Fatalln(err)
	}

	signature := minisign.Sign(privateKey, message)
	
	if !minisign.Verify(publicKey, message, signature) {
		log.Fatalln("signature verification failed")
	}
	log.Println(string(message))
}
```
For more examples visit the package [documentation](https://pkg.go.dev/aead.dev/minisign).
