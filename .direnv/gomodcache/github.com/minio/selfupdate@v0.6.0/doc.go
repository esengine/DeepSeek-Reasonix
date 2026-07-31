/*
Package update provides functionality to implement secure, self-updating Go programs (or other single-file targets).

For complete updating solutions please see Equinox (https://equinox.io) and go-tuf (https://github.com/flynn/go-tuf).

# Basic Example

This example shows how to update a program remotely from a URL.

	import (
		"fmt"
		"net/http"

		"github.com/minio/selfupdate"
	)

	func doUpdate(url string) error {
		// request the new file
		resp, err := http.Get(url)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		err := selfupdate.Apply(resp.Body, selfupdate.Options{})
		if err != nil {
			if rerr := selfupdate.RollbackError(err); rerr != nil {
				fmt.Println("Failed to rollback from bad update: %v", rerr)
			}
		}
		return err
	}

# Binary Patching

Go binaries can often be large. It can be advantageous to only ship a binary patch to a client
instead of the complete program text of a new version.

This example shows how to update a program with a bsdiff binary patch. Other patch formats
may be applied by implementing the Patcher interface.

	import (
		"encoding/hex"
		"io"

		"github.com/minio/selfupdate"
	)

	func updateWithPatch(patch io.Reader) error {
		err := selfupdate.Apply(patch, selfupdate.Options{
			Patcher: selfupdate.NewBSDiffPatcher()
		})
		if err != nil {
			// error handling
		}
		return err
	}

# Checksum Verification

Updating executable code on a computer can be a dangerous operation unless you
take the appropriate steps to guarantee the authenticity of the new code. While
checksum verification is important, it should always be combined with signature
verification (next section) to guarantee that the code came from a trusted party.

selfupdate validates SHA256 checksums by default, but this is pluggable via the Hash
property on the Options struct.

This example shows how to guarantee that the newly-updated binary is verified to
have an appropriate checksum (that was otherwise retrieved via a secure channel)
specified as a hex string.

	import (
		"crypto"
		_ "crypto/sha256"
		"encoding/hex"
		"io"

		"github.com/minio/selfupdate"
	)

	func updateWithChecksum(binary io.Reader, hexChecksum string) error {
		checksum, err := hex.DecodeString(hexChecksum)
		if err != nil {
			return err
		}
		err = selfupdate.Apply(binary, selfupdate.Options{
			Hash: crypto.SHA256, 	// this is the default, you don't need to specify it
			Checksum: checksum,
		})
		if err != nil {
			// error handling
		}
		return err
	}

# Cryptographic Signature Verification

Cryptographic verification of new code from an update is an extremely important way to guarantee the
security and integrity of your updates.

Verification is performed by validating the signature of a hash of the new file. This
means nothing changes if you apply your update with a patch.

This example shows how to add signature verification to your updates. To make all of this work
an application distributor must first create a public/private key pair and embed the public key
into their application. When they issue a new release, the issuer must sign the new executable file
with the private key and distribute the signature along with the selfupdate.

	import (
		"crypto"
		_ "crypto/sha256"
		"encoding/hex"
		"io"

		"github.com/minio/selfupdate"
	)

	func verifiedUpdate(binary io.Reader, hexChecksum string) {
		checksum, err := hex.DecodeString(hexChecksum)
		if err != nil {
			return err
		}
		opts := selfupdate.Options{
			Checksum: checksum,
			Hash: crypto.SHA256,                // this is the default, you don't need to specify it
		}
		err = selfupdate.Apply(binary, opts)
		if err != nil {
			// error handling
		}
		return err
	}

# Building Single-File Go Binaries

In order to update a Go application with selfupdate, you must distribute it as a single executable.
This is often easy, but some applications require static assets (like HTML and CSS asset files or TLS certificates).
In order to update applications like these, you'll want to make sure to embed those asset files into
the distributed binary with a tool like go-bindata (my favorite): https://github.com/jteeuwen/go-bindata

# Non-Goals

Mechanisms and protocols for determining whether an update should be applied and, if so, which one are
out of scope for this package. Please consult go-tuf (https://github.com/flynn/go-tuf) or Equinox (https://equinox.io)
for more complete solutions.

selfupdate only works for self-updating applications that are distributed as a single binary, i.e.
applications that do not have additional assets or dependency files.
Updating application that are distributed as multiple on-disk files is out of scope, although this
may change in future versions of this library.
*/
package selfupdate
