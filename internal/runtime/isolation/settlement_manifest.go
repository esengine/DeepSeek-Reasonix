package isolation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

const settlementManifestVersion = 1
const settlementWriterProtocol = "per-result-fence-v1"
const settlementManifestMaxBytes = 64 << 20
const settlementManifestMaxFiles = 50_000

var errSettlementManifest = errors.New("unsupported settlement manifest; original bytes retained")

type settlementIdentity struct {
	SourceID    string `json:"source_id"`
	ResultID    string `json:"result_id"`
	SessionID   string `json:"session_id"`
	ExecutionID string `json:"execution_id"`
	Generation  uint64 `json:"generation"`
	AttemptID   string `json:"attempt_id"`
}

type settlementImage struct {
	Mode   uint32 `json:"mode"`
	Data   []byte `json:"data"`
	SHA256 string `json:"sha256"`
}

type settlementFile struct {
	// Path is Git spelling, not host filesystem authorization.
	Path   string          `json:"path"`
	Before settlementImage `json:"before"`
	After  settlementImage `json:"after"`
}

// The manifest is immutable input, not progress or permission to publish files.
// Future progress records must bind its digest without changing these bytes.
type settlementManifest struct {
	Version        int                `json:"version"`
	WriterProtocol string             `json:"writer_protocol"`
	Identity       settlementIdentity `json:"identity"`
	Files          []settlementFile   `json:"files"`
}

func (m settlementManifest) encode() ([]byte, string, error) {
	if err := m.validate(); err != nil {
		return nil, "", err
	}
	m.Files = slices.Clone(m.Files)
	slices.SortFunc(m.Files, func(a, b settlementFile) int { return strings.Compare(a.Path, b.Path) })
	for i := range m.Files {
		if i > 0 && m.Files[i-1].Path == m.Files[i].Path {
			return nil, "", manifestError("duplicate file path")
		}
		if len(m.Files[i].Before.Data) == 0 {
			m.Files[i].Before.Data = nil
		}
		if len(m.Files[i].After.Data) == 0 {
			m.Files[i].After.Data = nil
		}
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return nil, "", errors.Join(errSettlementManifest, err)
	}
	if len(raw) > settlementManifestMaxBytes {
		return nil, "", manifestError("encoded size exceeds limit")
	}
	return raw, settlementDigest(raw), nil
}

func decodeSettlementManifest(raw []byte, expected settlementIdentity, revision string) (settlementManifest, string, error) {
	if len(raw) > settlementManifestMaxBytes {
		return settlementManifest{}, "", manifestError("encoded size exceeds limit")
	}
	if !validSettlementDigest(revision) || settlementDigest(raw) != revision {
		return settlementManifest{}, "", manifestError("preview revision differs")
	}
	var m settlementManifest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&m); err != nil {
		return settlementManifest{}, "", errors.Join(errSettlementManifest, err)
	}
	if m.Identity != expected {
		return settlementManifest{}, "", manifestError("execution identity differs")
	}
	canonical, digest, err := m.encode()
	if err != nil {
		return settlementManifest{}, "", err
	}
	// Machine-owned canonical bytes also exclude duplicate keys, case aliases,
	// trailing values and lossy UTF-8/base64 normalization by the JSON decoder.
	if !bytes.Equal(raw, canonical) {
		return settlementManifest{}, "", manifestError("noncanonical record")
	}
	return m, digest, nil
}

func (m settlementManifest) validate() error {
	if m.Version != settlementManifestVersion || m.WriterProtocol != settlementWriterProtocol {
		return manifestError("unknown version or writer protocol")
	}
	if !validSettlementDigest(m.Identity.SourceID) || m.Identity.Generation == 0 {
		return manifestError("missing source identity or generation")
	}
	for _, identity := range []string{m.Identity.ResultID, m.Identity.SessionID, m.Identity.ExecutionID, m.Identity.AttemptID} {
		if identity == "" || len(identity) > 256 || strings.TrimSpace(identity) != identity || !utf8.ValidString(identity) || strings.ContainsRune(identity, 0) {
			return manifestError("invalid execution identity")
		}
	}
	if len(m.Files) == 0 || len(m.Files) > settlementManifestMaxFiles {
		return manifestError("file count outside supported limits")
	}
	remaining := settlementManifestMaxBytes
	for _, file := range m.Files {
		if !validSettlementPath(file.Path) || len(file.Path) > remaining {
			return manifestError("invalid file path or size")
		}
		remaining -= len(file.Path)
		for _, image := range []settlementImage{file.Before, file.After} {
			if len(image.Data) > remaining || !image.valid() {
				return manifestError("invalid file image or size")
			}
			remaining -= len(image.Data)
		}
	}
	return nil
}

func validSettlementPath(path string) bool {
	if path == "" || !utf8.ValidString(path) || strings.ContainsRune(path, 0) {
		return false
	}
	for {
		part, rest, more := strings.Cut(path, "/")
		if part == "" || part == "." || part == ".." {
			return false
		}
		if !more {
			return true
		}
		path = rest
	}
}

func (image settlementImage) valid() bool {
	switch image.Mode {
	case 0:
		return len(image.Data) == 0 && image.SHA256 == ""
	case 0o100644, 0o100755:
	case 0o120000:
		if len(image.Data) == 0 || bytes.IndexByte(image.Data, 0) >= 0 {
			return false
		}
	default:
		return false
	}
	return validSettlementDigest(image.SHA256) && image.SHA256 == settlementDigest(image.Data)
}

func settlementDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func validSettlementDigest(digest string) bool {
	if len(digest) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(digest)
	return err == nil && len(decoded) == sha256.Size && hex.EncodeToString(decoded) == digest
}

func manifestError(reason string) error { return fmt.Errorf("%w: %s", errSettlementManifest, reason) }
