package update

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

var windowsPayloadTreeNames = []string{
	"app/Reasonix.exe",
	"app/resources/app.asar",
	"app/locales/en-US.pak",
}

func windowsPayloadTreeHashes() map[string]string {
	hashes := make(map[string]string, len(windowsPayloadFileNames)+len(windowsPayloadTreeNames))
	for _, name := range windowsPayloadFileNames {
		hashes[name] = WindowsPayloadSHA256([]byte(name))
	}
	for _, name := range windowsPayloadTreeNames {
		hashes[name] = WindowsPayloadSHA256([]byte(name))
	}
	return hashes
}

func TestWindowsPayloadManifestSchema2RoundTripsShellTree(t *testing.T) {
	hashes := windowsPayloadTreeHashes()
	b, err := EncodeWindowsPayloadManifest("v2.0.0", hashes)
	if err != nil {
		t.Fatal(err)
	}
	var manifest WindowsPayloadManifest
	if err := json.Unmarshal(b, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != 2 || WindowsPayloadManifestSchemaVersion != 2 {
		t.Fatalf("schema = %d (constant %d), want 2", manifest.SchemaVersion, WindowsPayloadManifestSchemaVersion)
	}
	names := make([]string, 0, len(manifest.Files))
	for _, file := range manifest.Files {
		names = append(names, file.Name)
	}
	if !slices.IsSorted(names) || len(names) != len(hashes) {
		t.Fatalf("manifest names = %v, want sorted exact set", names)
	}
	decoded, err := DecodeWindowsPayloadManifest(b, "v2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	for name, hash := range hashes {
		if decoded[name] != hash {
			t.Fatalf("decoded[%s] = %q, want %q", name, decoded[name], hash)
		}
	}
	want := []string{
		"app/Reasonix.exe",
		"app/locales/en-US.pak",
		"app/resources/app.asar",
		"reasonix-cli.exe",
		"reasonix-desktop.exe",
		"reasonix-update-helper.exe",
	}
	if got := WindowsPayloadVersionMembers(decoded); !slices.Equal(got, want) {
		t.Fatalf("version members = %v, want %v", got, want)
	}
}

func TestWindowsPayloadManifestAcceptsSchema1FlatList(t *testing.T) {
	var files []string
	for _, name := range windowsPayloadFileNames {
		files = append(files, `{"name":"`+name+`","sha256":"`+strings.Repeat("a", 64)+`"}`)
	}
	flat := `{"schemaVersion":1,"version":"v1","files":[` + strings.Join(files, ",") + `]}`
	hashes, err := DecodeWindowsPayloadManifest([]byte(flat), "v1")
	if err != nil {
		t.Fatalf("schema 1 manifest rejected: %v", err)
	}
	if len(hashes) != len(windowsPayloadFileNames) {
		t.Fatalf("schema 1 members = %d, want %d", len(hashes), len(windowsPayloadFileNames))
	}
	if got := WindowsPayloadVersionMembers(hashes); !slices.Equal(got, []string{"reasonix-cli.exe", "reasonix-desktop.exe", "reasonix-update-helper.exe"}) {
		t.Fatalf("schema 1 version members = %v", got)
	}
	tree := strings.Replace(flat, `"files":[`, `"files":[{"name":"app/Reasonix.exe","sha256":"`+strings.Repeat("a", 64)+`"},`, 1)
	if _, err := DecodeWindowsPayloadManifest([]byte(tree), "v1"); err == nil {
		t.Fatal("schema 1 manifest with a tree member was accepted")
	}
	if _, err := DecodeWindowsPayloadManifest([]byte(strings.Replace(flat, `"schemaVersion":1`, `"schemaVersion":3`, 1)), "v1"); err == nil {
		t.Fatal("unknown schema version was accepted")
	}
}

func TestWindowsPayloadManifestRejectsInvalidTreeNames(t *testing.T) {
	for _, name := range []string{
		"app/../reasonix-desktop.exe",
		`app\Reasonix.exe`,
		"lib/x.dll",
		"/app/x",
		"app/",
		"app",
		"app/./x",
		"App/x",
		"app/c:x",
		"app//x",
	} {
		hashes := windowsPayloadTreeHashes()
		hashes[name] = strings.Repeat("a", 64)
		if _, err := EncodeWindowsPayloadManifest("v2", hashes); err == nil {
			t.Fatalf("encode accepted tree name %q", name)
		}
		if ValidWindowsPayloadTreeName(name) {
			t.Fatalf("ValidWindowsPayloadTreeName(%q) = true", name)
		}
	}
	b, err := EncodeWindowsPayloadManifest("v2", windowsPayloadTreeHashes())
	if err != nil {
		t.Fatal(err)
	}
	escaped := strings.Replace(string(b), `"app/Reasonix.exe"`, `"app/../Reasonix.exe"`, 1)
	if _, err := DecodeWindowsPayloadManifest([]byte(escaped), "v2"); err == nil {
		t.Fatal("decode accepted a traversal tree name")
	}
}

func TestWindowsPayloadManifestSchema2RequiresFlatReleaseUnit(t *testing.T) {
	hashes := windowsPayloadTreeHashes()
	delete(hashes, "reasonix-guard.exe")
	if _, err := EncodeWindowsPayloadManifest("v2", hashes); err == nil {
		t.Fatal("manifest without the flat release unit was encoded")
	}
	b, err := EncodeWindowsPayloadManifest("v2", windowsPayloadTreeHashes())
	if err != nil {
		t.Fatal(err)
	}
	dropped := strings.Replace(string(b), `"name": "reasonix-guard.exe"`, `"name": "app/guard.exe"`, 1)
	if _, err := DecodeWindowsPayloadManifest([]byte(dropped), "v2"); err == nil {
		t.Fatal("manifest missing a flat member was decoded")
	}
}
