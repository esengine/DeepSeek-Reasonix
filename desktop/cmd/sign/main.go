// Command sign is the CI-side signing and manifest tool for desktop releases. It
// is never shipped in any artifact — the release workflow invokes it via
// `go run ./cmd/sign`. It shares desktop/internal/update with the running updater
// so the sign path and the verify path use one definition of the manifest and one
// minisign implementation.
//
// Subcommands:
//
//	sign <file>...               Write <file>.minisig for each file, signing with the
//	                             encrypted minisign private key in $MINISIGN_PRIVATE_KEY
//	                             (decrypted with $MINISIGN_PASSWORD).
//
//	manifest <dir> <ver> <tag>   Scan <dir> for the per-platform artifacts, compute
//	                             size + sha256, and write <dir>/latest.json with GitHub
//	                             release download URLs. The R2 mirror step rewrites those
//	                             URLs to the CDN afterwards (url + sig fields together).
package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"aead.dev/minisign"

	"reasonix/desktop/internal/update"
)

// platforms are the manifest keys we publish. A built artifact is matched to a key
// by substring (file names embed the key, e.g. Reasonix-darwin-arm64.zip), so the
// generator and the updater agree on update.PlatformKey output.
var platforms = []string{"darwin-arm64", "darwin-amd64", "windows-amd64", "windows-arm64", "linux-amd64"}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "sign":
		err = signFiles(os.Args[2:])
	case "manifest":
		if len(os.Args) != 5 {
			usage()
		}
		err = genManifest(os.Args[2], os.Args[3], os.Args[4])
	case "genkey":
		if len(os.Args) != 3 {
			usage()
		}
		err = genKey(os.Args[2])
	case "verify":
		if len(os.Args) != 3 {
			usage()
		}
		err = verifyFile(os.Args[2])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "sign:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage:\n  sign <file>...\n  manifest <dir> <version> <tag>\n  genkey <dir>\n  verify <file>")
	os.Exit(2)
}

// verifyFile checks <file> against <file>.minisig using the embedded public key —
// the same check the updater runs before applying. A self-test that the signing
// key matches what's compiled in. Returns an error (nonzero exit) on mismatch.
func verifyFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	sig, err := os.ReadFile(path + ".minisig")
	if err != nil {
		return err
	}
	if err := update.Verify(data, sig); err != nil {
		return err
	}
	fmt.Printf("OK: %s verifies against the embedded public key\n", path)
	return nil
}

// genKey generates a fresh minisign key pair, writing the encrypted private key
// (reasonix.key) and the public key (reasonix.pub) into dir. The password comes
// from $MINISIGN_PASSWORD. The public key is printed — it's safe to publish; embed
// it in internal/update/verify.go. The private key never leaves dir.
func genKey(dir string) error {
	pw := os.Getenv("MINISIGN_PASSWORD")
	if strings.TrimSpace(pw) == "" {
		return fmt.Errorf("genkey: MINISIGN_PASSWORD is empty")
	}
	pub, priv, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	enc, err := minisign.EncryptKey(pw, priv)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	keyPath := filepath.Join(dir, "reasonix.key")
	pubPath := filepath.Join(dir, "reasonix.pub")
	if err := os.WriteFile(keyPath, enc, 0o600); err != nil {
		return err
	}
	pubText, err := pub.MarshalText()
	if err != nil {
		return err
	}
	if err := os.WriteFile(pubPath, pubText, 0o644); err != nil {
		return err
	}
	fmt.Printf("private key -> %s (keep secret; this is the MINISIGN_PRIVATE_KEY value)\n", keyPath)
	fmt.Printf("public key  -> %s\n\n", pubPath)
	fmt.Printf("public key (embed in internal/update/verify.go, key ID %016X):\n%s\n", pub.ID(), pubText)
	return nil
}

// signFiles writes a detached .minisig next to each input file. The private key is
// read only from the environment — it never touches disk or argv.
func signFiles(files []string) error {
	if len(files) == 0 {
		return fmt.Errorf("sign: no files given")
	}
	keyText := os.Getenv("MINISIGN_PRIVATE_KEY")
	if strings.TrimSpace(keyText) == "" {
		return fmt.Errorf("sign: MINISIGN_PRIVATE_KEY is empty")
	}
	priv, err := minisign.DecryptKey(os.Getenv("MINISIGN_PASSWORD"), []byte(keyText))
	if err != nil {
		return fmt.Errorf("sign: decrypt private key: %w", err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		sig := minisign.SignWithComments(priv, data,
			"file:"+filepath.Base(f), "Reasonix desktop release")
		out := f + ".minisig"
		if err := os.WriteFile(out, sig, 0o644); err != nil {
			return err
		}
		fmt.Printf("signed %s -> %s\n", f, out)
	}
	return nil
}

// genManifest scans dir for the per-platform artifacts and writes dir/latest.json.
// version is the semver compared by the updater (e.g. "v1.1.0"); tag is the GitHub
// release tag used in download URLs (e.g. "desktop-v1.1.0").
func genManifest(dir, version, tag string) error {
	repo := os.Getenv("GITHUB_REPOSITORY")
	if repo == "" || repo == "esengine/reasonix" {
		repo = "esengine/DeepSeek-Reasonix"
	}
	m := update.Manifest{
		Version:      version,
		DownloadPage: "https://reasonix.io/#start",
		Platforms:    map[string]update.Asset{},
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasSuffix(name, ".minisig") || name == "latest.json" {
			continue
		}
		key := matchPlatform(name)
		if key == "" {
			continue
		}
		size, sum, err := hashFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		url := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", repo, tag, name)
		m.Platforms[key] = update.Asset{URL: url, Sig: url + ".minisig", Size: size, SHA256: sum}
		fmt.Printf("manifest: %s -> %s (%d bytes)\n", key, name, size)
	}
	if len(m.Platforms) == 0 {
		return fmt.Errorf("manifest: no platform artifacts found in %s", dir)
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "latest.json"), append(b, '\n'), 0o644)
}

// matchPlatform returns the platform key embedded in a file name, or "" if none.
func matchPlatform(name string) string {
	// The .deb is a human-download package (like the macOS .dmg); the Linux updater
	// channel is the .tar.gz. Skip it so it doesn't shadow the tarball's linux-amd64 key.
	if strings.HasSuffix(name, ".deb") {
		return ""
	}
	// The Windows updater channel is the per-arch -installer.exe; the portable .zip
	// is a human download, so skip it or it would shadow the installer's key.
	if strings.Contains(name, "windows-") && !strings.HasSuffix(name, "-installer.exe") {
		return ""
	}
	for _, p := range platforms {
		if strings.Contains(name, p) {
			return p
		}
	}
	return ""
}

// hashFile returns the size and lowercase-hex SHA-256 of a file, streaming it so
// large artifacts don't have to fit in memory.
func hashFile(path string) (int64, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return 0, "", err
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}
