package fileutil

import "os"

// StageAtomicWrite writes data to a temp file beside path, fsynced and with
// perm applied, and returns its name for PublishStagedWrite. A caller that
// abandons the staged file removes it.
func StageAtomicWrite(path string, data []byte, perm os.FileMode) (string, error) {
	return writeAtomicTemp(path, data, perm)
}

// PublishStagedWrite renames a staged file onto path under the strict rules
// of AtomicWriteFileStrict: atomic rename only, retried through transient
// locks, then a best-effort parent-directory fsync. A failed publish removes
// the staged file.
func PublishStagedWrite(tmpPath, path string) error {
	if err := replaceFile(tmpPath, path, false); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	_ = syncParentDirFn(path)
	return nil
}
