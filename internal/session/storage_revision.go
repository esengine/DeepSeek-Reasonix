package session

import (
	"fmt"
	"os"
	"path/filepath"
)

func (s *Store) ensureStorageRevision(revision int) (Manifest, error) {
	if s == nil {
		return Manifest{}, os.ErrClosed
	}
	if revision < StorageRevision || revision > MaxStorageRevision {
		return Manifest{}, fmt.Errorf("%w: storage revision %d", ErrUnsupportedVersion, revision)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.file == nil {
		return Manifest{}, os.ErrClosed
	}
	if s.manifest.StorageRevision >= revision {
		return s.manifest, nil
	}
	manifest := s.manifest
	manifest.StorageRevision = revision
	if err := writeManifestFile(filepath.Join(s.dir, "manifest.json"), manifest); err != nil {
		return Manifest{}, err
	}
	s.manifest = manifest
	return manifest, nil
}

func supportedStoredManifest(manifest Manifest) bool {
	if currentStoredManifest(manifest) {
		return true
	}
	return manifest.SchemaVersion == 3 &&
		(manifest.Codec == FinalV31Codec || manifest.Codec == LegacyLinearCodec || manifest.Codec == PrototypeCodec)
}

func currentStoredManifest(manifest Manifest) bool {
	return manifest.SchemaVersion == SchemaVersion && manifest.Codec == Codec &&
		manifest.StorageRevision >= 1 && manifest.StorageRevision <= MaxStorageRevision
}
