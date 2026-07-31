// Copyright (c) 2021 Andreas Auernhammer. All rights reserved.
// Use of this source code is governed by a license that can be
// found in the LICENSE file.

package main

import (
	"bufio"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"aead.dev/minisign"
	"golang.org/x/term"
)

const version = "v0.3.0"

const usage = `Usage:
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
`

var (
	flagKeyGen         bool // Generate a new key pair.
	flagRestore        bool // Restore a public key from a private key
	flagChangePassword bool // Update/Remove private key password
	flagSign           bool // Sign files
	flagVerify         bool // Verify signatures

	flagPrivateKeyFile string        // Path to private key file
	flagPublicKeyFile  string        // Path to public key flile
	flagPublicKey      string        // Public key. Takes precedence over public key file
	flagFiles          = filenames{} // List of files to sign/verify
	flagSignatureFile  string        // Custom signature file. Defaults to <file>.minisig

	flagTrustedComment   string // Custom comment that is signed and verified
	flagUntrustedComment string // Custom comment that is NOT signed NOR verified

	flagOutput          bool // Output files when verified successfully
	flagPreHash         bool // Verify legacy signatures when files where pre-hashed
	flagWithoutPassword bool // Whether a private key should be password-protected
	flagPrettyQuiet     bool // Suppress output except for trusted comment after verification
	flagQuiet           bool // Suppress all output
	flagForce           bool // Overwrite existing private/public keys
	flagVersion         bool // Print version information
)

func main() {
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }

	flag.BoolVar(&flagKeyGen, "G", false, "")
	flag.BoolVar(&flagRestore, "R", false, "")
	flag.BoolVar(&flagChangePassword, "C", false, "")
	flag.BoolVar(&flagSign, "S", false, "")
	flag.BoolVar(&flagVerify, "V", false, "")

	flag.StringVar(&flagPrivateKeyFile, "s", filepath.Join(homedir(), ".minisign/minisign.key"), "")
	flag.StringVar(&flagPublicKeyFile, "p", "minisign.pub", "")
	flag.StringVar(&flagPublicKey, "P", "", "")
	flag.Var(&flagFiles, "m", "")
	flag.StringVar(&flagSignatureFile, "x", "", "")

	flag.StringVar(&flagTrustedComment, "t", "", "")
	flag.StringVar(&flagUntrustedComment, "c", "", "")

	flag.BoolVar(&flagOutput, "o", false, "")
	flag.BoolVar(&flagPreHash, "H", false, "")
	flag.BoolVar(&flagWithoutPassword, "W", false, "")
	flag.BoolVar(&flagPrettyQuiet, "Q", false, "")
	flag.BoolVar(&flagQuiet, "q", false, "")
	flag.BoolVar(&flagForce, "f", false, "")
	flag.BoolVar(&flagVersion, "v", false, "")

	os.Args = append(os.Args[:1:1], expandFlags(os.Args[1:])...) // Expand flags to parse combined flags '-Vm' or '-Gf' properly
	flag.Parse()

	if flagVersion {
		fmt.Printf("minisign %s on %s-%s\n", version, runtime.GOOS, runtime.GOARCH)
		return
	}

	switch {
	case flagKeyGen:
		generateKeyPair()
	case flagRestore:
		restorePublicKey()
	case flagChangePassword:
		changePassword()
	case flagSign:
		signFiles()
	case flagVerify:
		verifyFile()
	default:
		flag.Usage()
		os.Exit(1)
	}
}

func generateKeyPair() {
	// Create private and public key parent directories
	mkdirs(filepath.Dir(flagPrivateKeyFile))
	mkdirs(filepath.Dir(flagPublicKeyFile))

	// Check whether private / public key already exists
	if !flagForce {
		if _, err := os.Stat(flagPrivateKeyFile); !errors.Is(err, os.ErrNotExist) {
			if err == nil {
				exitf("Error: %s already exists. Use -f if you really want to overwrite the existing key pair", flagPrivateKeyFile)
			}
			exitf("Error: %v", err)
		}
		if _, err := os.Stat(flagPublicKeyFile); !errors.Is(err, os.ErrNotExist) {
			if err == nil {
				exitf("Error: %s already exists. Use -f if you really want to overwrite the existing key pair", flagPublicKeyFile)
			}
			exitf("Error: %v", err)
		}
	}

	// Generate public / private key pair
	publicKey, privateKey, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		exitf("Error: %v", err)
	}
	pubKey, err := publicKey.MarshalText()
	if err != nil {
		exitf("Error: %v", err)
	}

	// Marshal or encrypt private key
	var privKey []byte
	if flagWithoutPassword {
		if privKey, err = privateKey.MarshalText(); err != nil {
			exitf("Error: %v", err)
		}
	} else {
		var password string
		if isTerm(os.Stdin) {
			fmt.Print("Please enter a password to protect the secret key.\n\n")
			password = readPassword(os.Stdin, "Password: ")
			passwordAgain := readPassword(os.Stdin, "Password (one more time): ")
			if password != passwordAgain {
				exit("Error: passwords don't match")
			}
		} else {
			password = readPassword(os.Stdin, "Password: ")
		}

		fmt.Print("Deriving a key from the password in order to encrypt the secret key... ")
		privKey, err = minisign.EncryptKey(password, privateKey)
		if err != nil {
			fmt.Println()
			exitf("Error: %v", err)
		}
		fmt.Print("done\n\n")
	}

	// Save public and private key
	if err = os.WriteFile(flagPrivateKeyFile, privKey, 0o600); err != nil {
		exitf("Error: %v", err)
	}
	if err = os.WriteFile(flagPublicKeyFile, pubKey, 0o644); err != nil {
		exitf("Error: %v", err)
	}

	var b = &strings.Builder{}
	fmt.Fprintf(b, "The secret key was saved as %s - Keep it secret!\n", flagPrivateKeyFile)
	fmt.Fprintf(b, "The public key was saved as %s - That one can be public.\n", flagPublicKeyFile)
	fmt.Fprintln(b)
	fmt.Fprintln(b, "Files signed using this key pair can be verified with the following command:")
	fmt.Fprintln(b)
	fmt.Fprintf(b, "minisign -Vm <file> -P %s\n", publicKey)
	fmt.Print(b)
}

func signFiles() {
	if len(flagFiles) == 0 {
		exit("Error: no files to sign. Use -m to specify one or more file paths")
	}
	if len(flagFiles) > 1 && flagSignatureFile != "" {
		exit("Error: -x cannot be used when more than one file should be signed")
	}

	var key minisign.PrivateKey
	keyBytes, err := os.ReadFile(flagPrivateKeyFile)
	if err != nil {
		exitf("Error: %v", err)
	}
	if minisign.IsEncrypted(keyBytes) {
		password := readPassword(os.Stdin, "Password: ")
		fmt.Print("Deriving a key from the password in order to decrypt the secret key... ")
		if key, err = minisign.DecryptKey(password, keyBytes); err != nil {
			fmt.Println()
			exitf("Error: invalid password: %v", err)
		}
		fmt.Print("done\n\n")
	} else if err = key.UnmarshalText(keyBytes); err != nil {
		exitf("Error: %v", err)
	}

	if flagSignatureFile != "" {
		mkdirs(filepath.Dir(flagSignatureFile))
	}
	for _, name := range flagFiles {
		tComment, uComment := flagTrustedComment, flagUntrustedComment
		if uComment == "" {
			uComment = "signature from minisign secret key"
		}
		if tComment == "" {
			tComment = fmt.Sprintf("timestamp:%d\tfilename:%s", time.Now().Unix(), filepath.Base(name))
		}

		file, err := os.Open(name)
		if err != nil {
			exitf("Error: %v", err)
		}
		if stat, _ := file.Stat(); stat != nil && stat.IsDir() {
			exitf("Error: %s is a directory", name)
		}

		reader := minisign.NewReader(file)
		_, err = io.Copy(io.Discard, reader)
		if _ = file.Close(); err != nil {
			exitf("Error: %v", err)
		}

		signature := reader.SignWithComments(key, tComment, uComment)
		signatureFile := flagSignatureFile
		if signatureFile == "" {
			signatureFile = name + ".minisig"
		}
		if err = os.WriteFile(signatureFile, signature, 0o644); err != nil {
			exitf("Error: %v", err)
		}
	}
}

func verifyFile() {
	if len(flagFiles) == 0 {
		exitf("Error: no files to verify. Use -m to specify a file path")
	}
	if len(flagFiles) > 1 {
		exitf("Error: too many files to verify. Only one file can be specified")
	}

	signatureFile := flagSignatureFile
	if signatureFile == "" {
		signatureFile = flagFiles[0] + ".minisig"
	}

	var publicKey minisign.PublicKey
	if flagPublicKey != "" {
		if err := publicKey.UnmarshalText([]byte(flagPublicKey)); err != nil {
			exitf("Error: invalid public key: %v", err)
		}
	} else {
		var err error
		if publicKey, err = minisign.PublicKeyFromFile(flagPublicKeyFile); err != nil {
			exitf("Error: %v", err)
		}
	}

	signature, err := minisign.SignatureFromFile(signatureFile)
	if err != nil {
		exitf("Error: %v", err)
	}
	if signature.KeyID != publicKey.ID() {
		exitf("Error: key IDs do not match. Try a different public key.\nID (public key): %X\nID (signature) : %X", publicKey.ID(), signature.KeyID)
	}

	rawSignature, err := signature.MarshalText()
	if err != nil {
		exitf("Error: %v", err)
	}
	if flagPreHash && signature.Algorithm != minisign.HashEdDSA {
		exit("Legacy (non-prehashed) signature found")
	}
	if signature.Algorithm == minisign.HashEdDSA || flagPreHash {
		file, err := os.Open(flagFiles[0])
		if err != nil {
			exitf("Error: %v", err)
		}
		defer file.Close()

		reader := minisign.NewReader(file)
		if _, err = io.Copy(io.Discard, reader); err != nil {
			exitf("Error: %v", err)
		}
		if !reader.Verify(publicKey, rawSignature) {
			exit("Error: signature verification failed")
		}
		if !flagQuiet {
			if !flagPrettyQuiet {
				fmt.Println("Signature and comment signature verified")
			}
			fmt.Println("Trusted comment:", signature.TrustedComment)
		}

		if flagOutput {
			if _, err = file.Seek(0, io.SeekStart); err != nil {
				exitf("Error: %v", err)
			}
			if _, err = io.Copy(os.Stdout, bufio.NewReader(file)); err != nil {
				exitf("Error: %v", err)
			}
		}
		return
	}

	message, err := os.ReadFile(flagFiles[0])
	if err != nil {
		exitf("Error: %v", err)
	}
	if !minisign.Verify(publicKey, message, rawSignature) {
		exit("Error: signature verification failed")
	}
	if !flagQuiet {
		if !flagPrettyQuiet {
			fmt.Println("Signature and comment signature verified")
		}
		fmt.Println("Trusted comment:", signature.TrustedComment)
	}
	if flagOutput {
		os.Stdout.Write(message)
	}
}

func restorePublicKey() {
	if !flagForce {
		if _, err := os.Stat(flagPublicKeyFile); err == nil {
			exitf("Error: %s already exists. Use -f if you really want to overwrite the existing key pair", flagPublicKeyFile)
		}
	}

	var privateKey minisign.PrivateKey
	keyBytes, err := os.ReadFile(flagPrivateKeyFile)
	if err != nil {
		exitf("Error: %v", err)
	}
	if minisign.IsEncrypted(keyBytes) {
		password := readPassword(os.Stdin, "Password: ")
		fmt.Print("Deriving a key from the password in order to decrypt the secret key... ")
		if privateKey, err = minisign.DecryptKey(password, keyBytes); err != nil {
			fmt.Println()
			exitf("Error: invalid password: %v", err)
		}
		fmt.Println("done")
	} else if err = privateKey.UnmarshalText(keyBytes); err != nil {
		exitf("Error: %v", err)
	}

	publicKey, err := privateKey.Public().(minisign.PublicKey).MarshalText()
	if err != nil {
		exitf("Error: %v", err)
	}
	if err = os.WriteFile(flagPublicKeyFile, publicKey, 0o644); err != nil {
		exitf("Error: %v", err)
	}
}

func changePassword() {
	keyBytes, err := os.ReadFile(flagPrivateKeyFile)
	if err != nil {
		exitf("Error: %v", err)
	}

	// minisign always prints this message - even if the private key is not encrypted
	if flagWithoutPassword {
		fmt.Printf("Key encryption for [%s] is going to be removed.\n", flagPrivateKeyFile)
	}

	// Unmarshal or decrypt private key
	var privateKey minisign.PrivateKey
	if minisign.IsEncrypted(keyBytes) {
		password := readPassword(os.Stdin, "Password: ")
		fmt.Print("Deriving a key from the password in order to decrypt the secret key... ")
		privateKey, err = minisign.DecryptKey(password, keyBytes)
		if err != nil {
			fmt.Println()
			exitf("Error: invalid password: %v", err)
		}
		fmt.Print("done\n\n")
	} else if err = privateKey.UnmarshalText(keyBytes); err != nil {
		exitf("Error: %v", err)
	}

	// Marshal or encrypt private key
	if flagWithoutPassword {
		if keyBytes, err = privateKey.MarshalText(); err != nil {
			exitf("Error: %v", err)
		}
	} else {
		var password string
		if isTerm(os.Stdin) {
			fmt.Print("Please enter a password to protect the secret key.\n\n")
			password = readPassword(os.Stdin, "Password: ")
			passwordAgain := readPassword(os.Stdin, "Password (one more time): ")
			if password != passwordAgain {
				exit("Error: passwords don't match")
			}
		} else {
			password = readPassword(os.Stdin, "Password: ")
		}

		fmt.Print("Deriving a key from the password in order to encrypt the secret key... ")
		if keyBytes, err = minisign.EncryptKey(password, privateKey); err != nil {
			fmt.Println()
			exitf("Error: %v", err)
		}
	}

	// Save private key. Use rename to prevent corrupting a private on write failure.
	if err = os.WriteFile(flagPrivateKeyFile+".tmp", keyBytes, 0o600); err != nil {
		exitf("Error: %v", err)
	}
	if err = os.Rename(flagPrivateKeyFile+".tmp", flagPrivateKeyFile); err != nil {
		exitf("Error: %v", err)
	}
	if flagWithoutPassword {
		fmt.Println("Password removed.") // Again, minisign always prints this message
	} else {
		fmt.Println("done\n\nPassword updated.")
	}
}

type filenames []string

var _ flag.Value = (*filenames)(nil) // compiler check

func (f *filenames) String() string { return fmt.Sprint(*f) }

func (f *filenames) Set(value string) error {
	*f = append(*f, value)
	return nil
}

// expandFlags expands args such that the flag package can parse them.
// For example, the arguments '-Voqm foo.txt bar.txt' are expanded to
// '-V -o -q -m foo.txt bar.txt'.
func expandFlags(args []string) []string {
	expArgs := make([]string, 0, len(args))
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			expArgs = append(expArgs, arg)
			continue
		}

		if len(arg) > 2 {
			expArgs = append(expArgs, arg[:2])
			for _, a := range arg[2:] {
				expArgs = append(expArgs, "-"+string(a))
			}
		} else {
			expArgs = append(expArgs, arg)
		}
	}
	return expArgs
}

// homedir returns the platform's user home directory.
// If no home directory can be detected, it aborts the
// program.
func homedir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		exitf("Error: failed to detect home directory: %v", err)
	}
	return home
}

// mkdirs creates the directory p, and any non-existing
// parent directories, unless p is empty, "." or a single
// path separator.
func mkdirs(p string) {
	if p == "" {
		return
	}
	if len(p) > 1 || (p[0] != '.' && !os.IsPathSeparator(p[0])) {
		if err := os.Mkdir(p, 0o755); !errors.Is(err, os.ErrExist) {
			if errors.Is(err, os.ErrNotExist) {
				err = os.MkdirAll(p, 0o755)
			}
			if err != nil {
				exitf("Error: %v", err)
			}
		}
	}
}

// readPassword reads a password from the file descriptor.
// If file is a terminal, it prints the message before waiting
// for the user to enter the password.
func readPassword(file *os.File, message string) string {
	if !isTerm(file) { // If file is not a terminal read the password directly from it
		p, err := bufio.NewReader(file).ReadString('\n')
		if err != nil {
			exitf("Error: failed to read password: %v", err)
		}

		// ReadString returns a string with the trailing newline
		if strings.HasSuffix(p, "\r\n") {
			return strings.TrimSuffix(p, "\r\n") // windows
		}
		return strings.TrimSuffix(p, "\n") // unix
	}

	fmt.Fprint(file, message)
	p, err := term.ReadPassword(int(file.Fd()))
	fmt.Fprintln(file)

	if err != nil {
		exitf("Error: failed to read password: %v", err)
	}
	return string(p)
}

// isTerm reports whether fd is a terminal
func isTerm(fd *os.File) bool { return term.IsTerminal(int(fd.Fd())) }

// exit formats and prints its args to stderr before exiting
// the program.
func exit(args ...any) {
	fmt.Fprintln(os.Stderr, args...)
	os.Exit(1)
}

// exitf formats and prints its args to stderr before exiting
// the program.
func exitf(format string, args ...any) {
	fmt.Fprintln(os.Stderr, fmt.Sprintf(format, args...))
	os.Exit(1)
}
