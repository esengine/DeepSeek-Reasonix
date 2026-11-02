package isolation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/platform/worktree"
)

// Mutable progress selects complete immutable images by revision. Neither the
// candidate path nor its current bytes can replace the originally selected ID.
type candidateProgress struct {
	Kind             string         `json:"kind"`
	ID               string         `json:"id"`
	Workspace        string         `json:"workspace"`
	SourceCommon     string         `json:"source_common"`
	IdentityRevision string         `json:"identity_revision"`
	ResultRevision   string         `json:"result_revision,omitempty"`
	Phase            candidatePhase `json:"phase"`
	Disposition      string         `json:"disposition,omitempty"`
	Updated          time.Time      `json:"updated"`
}

type candidateImage struct {
	Entry   durableEntry      `json:"entry"`
	Anchors candidateAnchors  `json:"anchors"`
	Tree    string            `json:"tree,omitempty"`
	Changes []worktree.Change `json:"changes,omitempty"`
}

func candidateImageName(id, revision string) (string, error) {
	bytes, err := hex.DecodeString(revision)
	if err != nil || len(bytes) != sha256.Size || strings.ToLower(revision) != revision {
		return "", errUnavailable
	}
	return candidateRecordName(id) + "." + revision + ".image", nil
}

func readCandidateImage(root *os.Root, id, revision string) (candidateImage, error) {
	var image candidateImage
	name, err := candidateImageName(id, revision)
	if err != nil || !recordRegularFile(root, name) {
		return image, errUnavailable
	}
	raw, err := readCandidateRecord(root, name)
	if err != nil {
		return image, err
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != revision || decodeCandidateJSON(raw, &image) != nil || image.Entry.ID != id {
		return image, errUnavailable
	}
	return image, nil
}

func writeCandidateImage(root *os.Root, id string, image candidateImage) (string, error) {
	raw, err := json.Marshal(image)
	if err != nil {
		return "", err
	}
	if len(raw) > maxCandidateRecordBytes {
		return "", errUnavailable
	}
	digest := sha256.Sum256(raw)
	revision := hex.EncodeToString(digest[:])
	name, err := candidateImageName(id, revision)
	if err != nil {
		return "", err
	}
	if _, err := root.Lstat(name); err == nil {
		_, err = readCandidateImage(root, id, revision)
		return revision, err
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	tmp, err := writeCandidateTemp(root, name, raw)
	if err != nil {
		return "", err
	}
	defer func() { _ = root.Remove(tmp) }()
	// The immutable image is visible only once complete, and never overwritten.
	if err := root.Link(tmp, name); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return "", err
		}
		_, err = readCandidateImage(root, id, revision)
		return revision, err
	}
	return revision, nil
}

func readCandidateProgress(root *os.Root, id string) (candidateProgress, error) {
	var progress candidateProgress
	name := candidateRecordName(id)
	if !recordRegularFile(root, name) {
		return progress, managedRecordRefusal(id)
	}
	raw, err := readCandidateRecord(root, name)
	if err != nil {
		return progress, errors.Join(managedRecordRefusal(id), err)
	}
	if decodeCandidateJSON(raw, &progress) != nil || progress.Kind != candidateRecordKind || progress.ID != id {
		return progress, managedRecordRefusal(id)
	}
	return progress, nil
}

func decodeCandidateJSON(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errUnavailable
	}
	return nil
}

func (s *Store) decodeManagedRecord(root *os.Root, id string) (candidateRecord, error) {
	var record candidateRecord
	p, err := readCandidateProgress(root, id)
	if err != nil {
		return record, err
	}
	identity, err := readCandidateImage(root, id, p.IdentityRevision)
	if err != nil || identity.Tree != "" || len(identity.Changes) != 0 || !s.managedIdentity(identity.Entry) {
		return record, managedRecordRefusal(id)
	}
	if p.Workspace != candidateWorkspace(identity.Entry) || p.SourceCommon != identity.Entry.Snap.Repo.CommonDir {
		return record, managedRecordRefusal(id)
	}
	record = candidateRecord{Kind: p.Kind, Entry: identity.Entry, Anchors: identity.Anchors, Phase: p.Phase, Disposition: p.Disposition, Updated: p.Updated}
	if !record.Anchors.sourceMatches(record.Entry) {
		return record, managedRecordRefusal(id)
	}
	if p.ResultRevision != "" {
		image, err := readCandidateImage(root, id, p.ResultRevision)
		if err != nil || image.Entry != identity.Entry || image.Anchors != identity.Anchors {
			return record, managedRecordRefusal(id)
		}
		record.Tree, record.Changes = image.Tree, image.Changes
	}
	return record, nil
}

func candidateWorkspace(entry durableEntry) string {
	return filepath.Join(entry.Snap.RepoRoot, filepath.FromSlash(entry.Snap.Prefix))
}

func writeCandidateTemp(root *os.Root, name string, raw []byte) (string, error) {
	id, err := newID()
	if err != nil {
		return "", err
	}
	tmp := name + "." + id + ".tmp"
	f, err := root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	_, writeErr := f.Write(raw)
	syncErr := f.Sync()
	closeErr := f.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		_ = root.Remove(tmp)
		return "", err
	}
	return tmp, nil
}
