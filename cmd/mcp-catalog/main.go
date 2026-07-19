package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"aead.dev/minisign"

	"reasonix/internal/mcpcatalog"
)

func main() {
	if len(os.Args) < 2 {
		fail("usage: mcp-catalog validate|sign [flags]")
	}
	switch os.Args[1] {
	case "validate":
		validate(os.Args[2:])
	case "sign":
		sign(os.Args[2:])
	default:
		fail("unknown command %q", os.Args[1])
	}
}

func validate(args []string) {
	flags := flag.NewFlagSet("validate", flag.ExitOnError)
	indexPath := flags.String("index", "internal/mcpcatalog/catalog-v1.json", "catalog source JSON")
	packagesDir := flags.String("packages", "", "directory containing one package tree per catalog entry ID")
	signaturePath := flags.String("signature", "", "optional minisign signature to verify")
	_ = flags.Parse(args)
	data := mustRead(*indexPath)
	index, err := mcpcatalog.Parse(data)
	if err != nil {
		fail("validate catalog: %v", err)
	}
	if strings.TrimSpace(*signaturePath) != "" {
		if _, err := mcpcatalog.Verify(data, mustRead(*signaturePath), mcpcatalog.PublicKeys); err != nil {
			fail("verify catalog signature: %v", err)
		}
	}
	if strings.TrimSpace(*packagesDir) != "" {
		for _, entry := range index.Entries {
			root := filepath.Join(*packagesDir, entry.ID)
			digest, err := mcpcatalog.TreeSHA256(root)
			if err != nil {
				fail("hash package %s: %v", entry.ID, err)
			}
			if !strings.EqualFold(digest, entry.PackageSHA256) {
				fail("package %s digest mismatch: got %s want %s", entry.ID, digest, entry.PackageSHA256)
			}
			manifest, err := manifestDigest(root)
			if err != nil {
				fail("hash package %s manifest: %v", entry.ID, err)
			}
			if !strings.EqualFold(manifest, entry.ManifestSHA256) {
				fail("package %s manifest digest mismatch: got %s want %s", entry.ID, manifest, entry.ManifestSHA256)
			}
		}
	}
	fmt.Printf("validated MCP catalog sequence %d with %d entries\n", index.Sequence, len(index.Entries))
}

func sign(args []string) {
	flags := flag.NewFlagSet("sign", flag.ExitOnError)
	indexPath := flags.String("index", "internal/mcpcatalog/catalog-v1.json", "catalog source JSON")
	outputPath := flags.String("output", "internal/mcpcatalog/catalog-v1.json.minisig", "signature output")
	_ = flags.Parse(args)
	data := mustRead(*indexPath)
	if _, err := mcpcatalog.Parse(data); err != nil {
		fail("validate catalog before signing: %v", err)
	}
	keyText := os.Getenv("MCP_CATALOG_MINISIGN_PRIVATE_KEY")
	if strings.TrimSpace(keyText) == "" {
		fail("MCP_CATALOG_MINISIGN_PRIVATE_KEY is required")
	}
	key, err := minisign.DecryptKey(os.Getenv("MCP_CATALOG_MINISIGN_PASSWORD"), []byte(keyText))
	if err != nil {
		var plain minisign.PrivateKey
		if plainErr := plain.UnmarshalText([]byte(keyText)); plainErr != nil {
			fail("parse catalog signing key: encrypted=%v plain=%v", err, plainErr)
		}
		key = plain
	}
	signature := minisign.Sign(key, data)
	if err := os.WriteFile(*outputPath, signature, 0o600); err != nil {
		fail("write signature: %v", err)
	}
	if _, err := mcpcatalog.Verify(data, signature, mcpcatalog.PublicKeys); err != nil {
		_ = os.Remove(*outputPath)
		fail("new signature does not match the app key ring: %v", err)
	}
	fmt.Printf("signed %s\n", *indexPath)
}

func manifestDigest(root string) (string, error) {
	for _, rel := range []string{"reasonix-plugin.json", ".codex-plugin/plugin.json", ".claude-plugin/plugin.json"} {
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err == nil {
			sum := sha256.Sum256(body)
			return hex.EncodeToString(sum[:]), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
	}
	return "", fmt.Errorf("plugin manifest not found")
}

func mustRead(path string) []byte {
	data, err := os.ReadFile(path)
	if err != nil {
		fail("read %s: %v", path, err)
	}
	return data
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
