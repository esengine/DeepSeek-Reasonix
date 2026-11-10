package update

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestDiscardDownloadRemovesPartialsAndTheNamedVersion(t *testing.T) {
	dir := testenv.TempDir(t)
	for _, name := range []string{
		partialPrefix + "aaaa" + partialSuffix,
		partialPrefix + "bbbb" + partialSuffix,
		"Reasonix-2.20.0-linux-something.tar.gz",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	data := []byte("artifact")
	sum := sha256.Sum256(data)
	asset := Asset{
		URL:    "https://example.com/Reasonix-2.21.0-" + CurrentPlatform() + ".tar.gz",
		Size:   int64(len(data)),
		SHA256: hex.EncodeToString(sum[:]),
	}
	cached, err := (Cache{Dir: dir}).Save("2.21.0", asset, data, KindTarball, nil)
	if err != nil {
		t.Fatal(err)
	}

	DiscardDownload(dir, "2.20.0")
	if _, err := os.Stat(cached.Path); err != nil {
		t.Fatalf("another version's artifact was removed: %v", err)
	}

	DiscardDownload(dir, "2.21.0")
	for _, name := range []string{
		partialPrefix + "aaaa" + partialSuffix,
		partialPrefix + "bbbb" + partialSuffix,
	} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("partial %s outlived the cancellation: %v", name, err)
		}
	}
	if _, err := os.Stat(cached.Path); !os.IsNotExist(err) {
		t.Fatalf("the cancelled artifact outlived the cancellation: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "downloaded.json")); !os.IsNotExist(err) {
		t.Fatalf("the metadata outlived the cancellation: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Reasonix-2.20.0-linux-something.tar.gz")); err != nil {
		t.Fatalf("an unrelated file was removed: %v", err)
	}
}

func TestDiscardDownloadToleratesAnEmptyDirectory(t *testing.T) {
	DiscardDownload("", "2.21.0")
	DiscardDownload(testenv.TempDir(t), "2.21.0")
}
