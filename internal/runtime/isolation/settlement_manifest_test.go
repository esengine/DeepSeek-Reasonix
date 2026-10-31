package isolation

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
)

func manifestImage(mode uint32, data []byte) settlementImage {
	if mode == 0 {
		return settlementImage{}
	}
	return settlementImage{Mode: mode, Data: slices.Clone(data), SHA256: settlementDigest(data)}
}

func testSettlementManifest() settlementManifest {
	return settlementManifest{
		Version: settlementManifestVersion, WriterProtocol: settlementWriterProtocol,
		Identity: settlementIdentity{SourceID: settlementDigest([]byte("host-bound-source")), ResultID: "iso_12345678",
			SessionID: "session", ExecutionID: "execution", Generation: 1, AttemptID: "attempt"},
		Files: []settlementFile{
			{Path: "z.txt", Before: manifestImage(0o100644, []byte("old\r\n")), After: manifestImage(0o100644, []byte("new\r\n"))},
			{Path: "a.txt", Before: manifestImage(0, nil), After: manifestImage(0o100755, []byte{0, 0xff, '\n'})},
		},
	}
}

func TestSettlementManifestRoundTripRetainsBytesAndModes(t *testing.T) {
	m := testSettlementManifest()
	m.Files = append(m.Files,
		settlementFile{Path: "removed", Before: manifestImage(0o100644, []byte("gone")), After: manifestImage(0, nil)},
		settlementFile{Path: "empty", Before: manifestImage(0, nil), After: manifestImage(0o100644, []byte{})},
		settlementFile{Path: "link", Before: manifestImage(0o100644, []byte("target")), After: manifestImage(0o120000, []byte("target"))},
	)
	raw, revision, err := m.encode()
	if err != nil {
		t.Fatal(err)
	}
	if m.Files[0].Path != "z.txt" {
		t.Fatal("encoder mutated caller ordering")
	}
	decoded, observed, err := decodeSettlementManifest(raw, m.Identity, revision)
	if err != nil || observed != revision {
		t.Fatalf("decode: %v, revision=%q", err, observed)
	}
	byPath := map[string]settlementFile{}
	for _, file := range decoded.Files {
		byPath[file.Path] = file
	}
	for _, expected := range m.Files {
		actual, ok := byPath[expected.Path]
		if !ok || actual.Before.Mode != expected.Before.Mode || actual.After.Mode != expected.After.Mode ||
			!bytes.Equal(actual.Before.Data, expected.Before.Data) || !bytes.Equal(actual.After.Data, expected.After.Data) {
			t.Fatalf("image lost for %q: %+v", expected.Path, actual)
		}
	}
	if byPath["empty"].Before.Mode == byPath["empty"].After.Mode || byPath["link"].Before.Mode == byPath["link"].After.Mode {
		t.Fatal("empty/missing or regular/link identity collapsed")
	}
	m.Files[0].After.Data[0] = 'X'
	if string(byPath["z.txt"].After.Data) != "new\r\n" {
		t.Fatal("decoded content aliases caller images")
	}
	raw[0] = '!'
	if string(byPath["z.txt"].After.Data) != "new\r\n" {
		t.Fatal("decoded content aliases input serialization")
	}
}

func TestSettlementManifestRevisionBindsEveryIdentityAndImage(t *testing.T) {
	base := testSettlementManifest()
	_, expected, err := base.encode()
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*settlementManifest){
		"source":     func(m *settlementManifest) { m.Identity.SourceID = settlementDigest([]byte("other source")) },
		"result":     func(m *settlementManifest) { m.Identity.ResultID = "iso_87654321" },
		"session":    func(m *settlementManifest) { m.Identity.SessionID = "other session" },
		"execution":  func(m *settlementManifest) { m.Identity.ExecutionID = "other execution" },
		"generation": func(m *settlementManifest) { m.Identity.Generation++ },
		"attempt":    func(m *settlementManifest) { m.Identity.AttemptID = "other attempt" },
		"path":       func(m *settlementManifest) { m.Files[0].Path = "another.txt" },
		"mode":       func(m *settlementManifest) { m.Files[0].After.Mode = 0o100755 },
		"before":     func(m *settlementManifest) { m.Files[0].Before = manifestImage(0o100644, []byte("other before")) },
		"after":      func(m *settlementManifest) { m.Files[0].After = manifestImage(0o100644, []byte("other after")) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			m := testSettlementManifest()
			mutate(&m)
			raw, revision, err := m.encode()
			if err != nil || revision == expected {
				t.Fatalf("revision did not bind changed input: %q, %v", revision, err)
			}
			if m.Identity != base.Identity {
				if _, _, err := decodeSettlementManifest(raw, base.Identity, revision); !errors.Is(err, errSettlementManifest) {
					t.Fatalf("foreign execution accepted: %v", err)
				}
			}
			if _, _, err := decodeSettlementManifest(raw, m.Identity, expected); !errors.Is(err, errSettlementManifest) {
				t.Fatalf("changed input reused the old preview: %v", err)
			}
		})
	}
	base.Files[0], base.Files[1] = base.Files[1], base.Files[0]
	_, reordered, err := base.encode()
	if err != nil || reordered != expected {
		t.Fatalf("equivalent ordering changed revision: %q, %v", reordered, err)
	}
}

func TestSettlementManifestRejectsUnsupportedImagesAndIdentity(t *testing.T) {
	cases := map[string]func(*settlementManifest){
		"version":       func(m *settlementManifest) { m.Version++ },
		"protocol":      func(m *settlementManifest) { m.WriterProtocol = "future" },
		"generation":    func(m *settlementManifest) { m.Identity.Generation = 0 },
		"source":        func(m *settlementManifest) { m.Identity.SourceID = "bad" },
		"execution":     func(m *settlementManifest) { m.Identity.ExecutionID = "" },
		"identity size": func(m *settlementManifest) { m.Identity.SessionID = strings.Repeat("s", 257) },
		"identity utf8": func(m *settlementManifest) { m.Identity.SessionID = string([]byte{0xff}) },
		"duplicate":     func(m *settlementManifest) { m.Files[1].Path = m.Files[0].Path },
		"absolute":      func(m *settlementManifest) { m.Files[0].Path = "/outside" },
		"traversal":     func(m *settlementManifest) { m.Files[0].Path = "a/../outside" },
		"path nul":      func(m *settlementManifest) { m.Files[0].Path = "a\x00b" },
		"path utf8":     func(m *settlementManifest) { m.Files[0].Path = string([]byte{0xff}) },
		"directory":     func(m *settlementManifest) { m.Files[0].After.Mode = 0o040000 },
		"gitlink":       func(m *settlementManifest) { m.Files[0].After.Mode = 0o160000 },
		"absent bytes":  func(m *settlementManifest) { m.Files[0].After.Mode = 0 },
		"digest":        func(m *settlementManifest) { m.Files[0].After.SHA256 = settlementDigest([]byte("other")) },
		"link nul":      func(m *settlementManifest) { m.Files[0].After = manifestImage(0o120000, []byte("bad\x00target")) },
		"link empty":    func(m *settlementManifest) { m.Files[0].After = manifestImage(0o120000, nil) },
		"empty":         func(m *settlementManifest) { m.Files = nil },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			m := testSettlementManifest()
			mutate(&m)
			if _, _, err := m.encode(); !errors.Is(err, errSettlementManifest) {
				t.Fatalf("unsupported plan encoded: %v", err)
			}
		})
	}
}

func TestSettlementManifestRejectsUnknownOrAmbiguousJSONWithoutMutation(t *testing.T) {
	m := testSettlementManifest()
	raw, _, err := m.encode()
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string][]byte{
		"unknown":          bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":1,"extension":true`), 1),
		"unknown image":    bytes.Replace(raw, []byte(`"mode":`), []byte(`"extension":true,"mode":`), 1),
		"unknown identity": bytes.Replace(raw, []byte(`"source_id":`), []byte(`"extension":true,"source_id":`), 1),
		"future version":   bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":2`), 1),
		"unknown state":    bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":1,"state":"future"`), 1),
		"duplicate":        bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":99,"version":1`), 1),
		"case alias":       bytes.Replace(raw, []byte(`"version":1`), []byte(`"Version":1`), 1),
		"trailing":         append(slices.Clone(raw), []byte(` {}`)...),
		"whitespace":       append(slices.Clone(raw), '\n'),
		"truncated":        slices.Clone(raw[:len(raw)-1]),
		"null":             []byte(`null`),
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			original := slices.Clone(input)
			if _, _, err := decodeSettlementManifest(input, m.Identity, settlementDigest(input)); !errors.Is(err, errSettlementManifest) {
				t.Fatalf("ambiguous record accepted: %v", err)
			}
			if !bytes.Equal(input, original) {
				t.Fatal("decoder changed unsupported bytes")
			}
		})
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	delete(fields, "files")
	incomplete, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := decodeSettlementManifest(incomplete, m.Identity, settlementDigest(incomplete)); !errors.Is(err, errSettlementManifest) {
		t.Fatalf("missing images accepted: %v", err)
	}
}

func TestSettlementManifestSizeAndLiteralFilenameBoundary(t *testing.T) {
	m := testSettlementManifest()
	for _, path := range []string{"*.txt", ":(top)a.txt", "line\nfile", "quote\"file", "back\\slash", "目录/文件"} {
		m.Files[0].Path = path
		raw, revision, err := m.encode()
		if err != nil {
			t.Fatal(err)
		}
		decoded, _, err := decodeSettlementManifest(raw, m.Identity, revision)
		if err != nil || !slices.ContainsFunc(decoded.Files, func(f settlementFile) bool { return f.Path == path }) {
			t.Fatalf("literal Git spelling changed: %q, %v", path, err)
		}
	}
	oversize := make([]byte, settlementManifestMaxBytes+1)
	if _, _, err := decodeSettlementManifest(oversize, m.Identity, ""); !errors.Is(err, errSettlementManifest) {
		t.Fatalf("oversized record accepted: %v", err)
	}
	m.Files[0].After = manifestImage(0o100644, oversize)
	if _, _, err := m.encode(); !errors.Is(err, errSettlementManifest) {
		t.Fatalf("oversized images encoded: %v", err)
	}
	m.Files = make([]settlementFile, settlementManifestMaxFiles+1)
	if _, _, err := m.encode(); !errors.Is(err, errSettlementManifest) {
		t.Fatalf("excessive file count accepted: %v", err)
	}
}

func TestSettlementManifestVersionOneEncodingIsStable(t *testing.T) {
	m := settlementManifest{
		Version: 1, WriterProtocol: "per-result-fence-v1",
		Identity: settlementIdentity{SourceID: strings.Repeat("a", 64), ResultID: "iso_12345678",
			SessionID: "s", ExecutionID: "e", Generation: 1, AttemptID: "a"},
		Files: []settlementFile{{Path: "file", Before: settlementImage{}, After: manifestImage(0o100644, nil)}},
	}
	const golden = `{"version":1,"writer_protocol":"per-result-fence-v1","identity":{"source_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","result_id":"iso_12345678","session_id":"s","execution_id":"e","generation":1,"attempt_id":"a"},"files":[{"path":"file","before":{"mode":0,"data":null,"sha256":""},"after":{"mode":33188,"data":null,"sha256":"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"}}]}`
	raw, revision, err := m.encode()
	if err != nil || string(raw) != golden || revision != settlementDigest([]byte(golden)) {
		t.Fatalf("version-1 encoding drifted: %s, %v", raw, err)
	}
	if _, _, err := decodeSettlementManifest([]byte(golden), m.Identity, revision); err != nil {
		t.Fatalf("version-1 fixture rejected: %v", err)
	}
	if _, _, err := decodeSettlementManifest([]byte(golden), m.Identity, ""); !errors.Is(err, errSettlementManifest) {
		t.Fatalf("missing preview revision accepted: %v", err)
	}
}
