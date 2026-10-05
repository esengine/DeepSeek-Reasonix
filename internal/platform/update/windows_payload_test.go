package update

import (
	"strings"
	"testing"
)

func TestWindowsPayloadManifestRoundTripBindsVersionAndExactMembers(t *testing.T) {
	hashes := make(map[string]string, len(windowsPayloadFileNames))
	for _, name := range windowsPayloadFileNames {
		hashes[name] = WindowsPayloadSHA256([]byte(name))
	}
	b, err := EncodeWindowsPayloadManifest("v2.0.0", hashes)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeWindowsPayloadManifest(b, "v2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != len(hashes) {
		t.Fatalf("decoded members = %d, want %d", len(decoded), len(hashes))
	}
	if _, err := DecodeWindowsPayloadManifest(b, "v2.0.1"); err == nil {
		t.Fatal("payload manifest authorized a different release version")
	}
}

func TestWindowsPayloadManifestRejectsUnknownOrMissingMembers(t *testing.T) {
	hashes := make(map[string]string, len(windowsPayloadFileNames))
	for _, name := range windowsPayloadFileNames {
		hashes[name] = strings.Repeat("a", 64)
	}
	delete(hashes, windowsPayloadFileNames[0])
	if _, err := EncodeWindowsPayloadManifest("v2", hashes); err == nil {
		t.Fatal("incomplete payload manifest encoded successfully")
	}
	hashes[windowsPayloadFileNames[0]] = strings.Repeat("a", 64)
	hashes["other.exe"] = strings.Repeat("b", 64)
	if _, err := EncodeWindowsPayloadManifest("v2", hashes); err == nil {
		t.Fatal("payload manifest with an extra member encoded successfully")
	}
}

func TestWindowsPayloadManifestRejectsDuplicateAndTrailingData(t *testing.T) {
	hashes := make(map[string]string, len(windowsPayloadFileNames))
	for _, name := range windowsPayloadFileNames {
		hashes[name] = strings.Repeat("a", 64)
	}
	b, err := EncodeWindowsPayloadManifest("v2", hashes)
	if err != nil {
		t.Fatal(err)
	}
	duplicate := strings.Replace(
		string(b),
		`"files": [`,
		`"files": [{"name":"reasonix-desktop.exe","sha256":"`+strings.Repeat("a", 64)+`"},`,
		1,
	)
	if _, err := DecodeWindowsPayloadManifest([]byte(duplicate), "v2"); err == nil {
		t.Fatal("duplicate payload member was accepted")
	}
	if _, err := DecodeWindowsPayloadManifest(append(b, []byte(`{}`)...), "v2"); err == nil {
		t.Fatal("trailing JSON value was accepted")
	}
}

func TestWindowsPayloadManifestRejectsNonCanonicalIdentity(t *testing.T) {
	hashes := make(map[string]string, len(windowsPayloadFileNames))
	for _, name := range windowsPayloadFileNames {
		hashes[name] = strings.Repeat("a", 64)
	}
	b, err := EncodeWindowsPayloadManifest("v2", hashes)
	if err != nil {
		t.Fatal(err)
	}
	for _, changed := range [][]byte{
		[]byte(strings.Replace(string(b), `"version": "v2"`, `"version": " v2 "`, 1)),
		[]byte(strings.Replace(string(b), `"name": "reasonix-desktop.exe"`, `"name": "Reasonix-Desktop.exe"`, 1)),
		[]byte(strings.Replace(string(b), strings.Repeat("a", 64), strings.Repeat("A", 64), 1)),
	} {
		if _, err := DecodeWindowsPayloadManifest(changed, "v2"); err == nil {
			t.Fatalf("non-canonical payload manifest was accepted: %s", changed)
		}
	}
}
