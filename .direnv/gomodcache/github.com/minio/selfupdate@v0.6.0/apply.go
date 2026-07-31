package selfupdate

import (
	"bytes"
	"crypto"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/minio/selfupdate/internal/osext"
)

// Apply performs an update of the current executable or opts.TargetFile, with
// the contents of the given io.Reader. When the update fails, it is unlikely
// that old executable is corrupted, but still, applications need to check the
// returned error with RollbackError() and notify the user of the bad news and
// ask them to recover manually.
func Apply(update io.Reader, opts Options) error {
	err := PrepareAndCheckBinary(update, opts)
	if err != nil {
		return err
	}
	return CommitBinary(opts)
}

// PrepareAndCheckBinary reads the new binary content from io.Reader and performs the following actions:
//   1. If configured, applies the contents of the update io.Reader as a binary patch.
//   2. If configured, computes the checksum of the executable and verifies it matches.
//   3. If configured, verifies the signature with a public key.
//   4. Creates a new file, /path/to/.target.new with the TargetMode with the contents of the updated file
func PrepareAndCheckBinary(update io.Reader, opts Options) error {
	// get target path
	targetPath, err := opts.getPath()
	if err != nil {
		return err
	}

	var newBytes []byte
	if opts.Patcher != nil {
		if newBytes, err = opts.applyPatch(update, targetPath); err != nil {
			return err
		}
	} else {
		// no patch to apply, go on through
		if newBytes, err = ioutil.ReadAll(update); err != nil {
			return err
		}
	}

	// verify checksum if requested
	if opts.Checksum != nil {
		if err = opts.verifyChecksum(newBytes); err != nil {
			return err
		}
	}

	if opts.Verifier != nil {
		if err = opts.Verifier.Verify(newBytes); err != nil {
			return err
		}
	}

	// get the directory the executable exists in
	updateDir := filepath.Dir(targetPath)
	filename := filepath.Base(targetPath)

	// Copy the contents of newbinary to a new executable file
	newPath := filepath.Join(updateDir, fmt.Sprintf(".%s.new", filename))
	fp, err := os.OpenFile(newPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, opts.getMode())
	if err != nil {
		return err
	}
	defer fp.Close()

	_, err = io.Copy(fp, bytes.NewReader(newBytes))
	if err != nil {
		return err
	}

	// if we don't call fp.Close(), windows won't let us move the new executable
	// because the file will still be "in use"
	fp.Close()
	return nil
}

// CommitBinary moves the new executable to the location of the current executable or opts.TargetPath
// if specified. It performs the following operations:
//   1. Renames /path/to/target to /path/to/.target.old
//   2. Renames /path/to/.target.new to /path/to/target
//   3. If the final rename is successful, deletes /path/to/.target.old, returns no error. On Windows,
//      the removal of /path/to/target.old always fails, so instead Apply hides the old file instead.
//   4. If the final rename fails, attempts to roll back by renaming /path/to/.target.old
//      back to /path/to/target.
//
// If the roll back operation fails, the file system is left in an inconsistent state where there is
// no new executable file and the old executable file could not be be moved to its original location.
// In this case you should notify the user of the bad news and ask them to recover manually. Applications
// can determine whether the rollback failed by calling RollbackError, see the documentation on that function
// for additional detail.
func CommitBinary(opts Options) error {
	// get the directory the file exists in
	targetPath, err := opts.getPath()
	if err != nil {
		return err
	}

	updateDir := filepath.Dir(targetPath)
	filename := filepath.Base(targetPath)
	newPath := filepath.Join(updateDir, fmt.Sprintf(".%s.new", filename))

	// this is where we'll move the executable to so that we can swap in the updated replacement
	oldPath := opts.OldSavePath
	removeOld := opts.OldSavePath == ""
	if removeOld {
		oldPath = filepath.Join(updateDir, fmt.Sprintf(".%s.old", filename))
	}

	// delete any existing old exec file - this is necessary on Windows for two reasons:
	// 1. after a successful update, Windows can't remove the .old file because the process is still running
	// 2. windows rename operations fail if the destination file already exists
	_ = os.Remove(oldPath)

	// move the existing executable to a new file in the same directory
	err = os.Rename(targetPath, oldPath)
	if err != nil {
		return err
	}

	// move the new exectuable in to become the new program
	err = os.Rename(newPath, targetPath)

	if err != nil {
		// move unsuccessful
		//
		// The filesystem is now in a bad state. We have successfully
		// moved the existing binary to a new location, but we couldn't move the new
		// binary to take its place. That means there is no file where the current executable binary
		// used to be!
		// Try to rollback by restoring the old binary to its original path.
		rerr := os.Rename(oldPath, targetPath)
		if rerr != nil {
			return &rollbackErr{err, rerr}
		}

		return err
	}

	// move successful, remove the old binary if needed
	if removeOld {
		errRemove := os.Remove(oldPath)

		// windows has trouble with removing old binaries, so hide it instead
		if errRemove != nil {
			_ = hideFile(oldPath)
		}
	}

	return nil
}

// RollbackError takes an error value returned by Apply and returns the error, if any,
// that occurred when attempting to roll back from a failed update. Applications should
// always call this function on any non-nil errors returned by Apply.
//
// If no rollback was needed or if the rollback was successful, RollbackError returns nil,
// otherwise it returns the error encountered when trying to roll back.
func RollbackError(err error) error {
	if err == nil {
		return nil
	}
	if rerr, ok := err.(*rollbackErr); ok {
		return rerr.rollbackErr
	}
	return nil
}

type rollbackErr struct {
	error             // original error
	rollbackErr error // error encountered while rolling back
}

type Options struct {
	// TargetPath defines the path to the file to update.
	// The emptry string means 'the executable file of the running program'.
	TargetPath string

	// Create TargetPath replacement with this file mode. If zero, defaults to 0755.
	TargetMode os.FileMode

	// Checksum of the new binary to verify against. If nil, no checksum or signature verification is done.
	Checksum []byte

	// Verifier for signature verification. If nil, no signature verification is done.
	Verifier *Verifier

	// Use this hash function to generate the checksum. If not set, SHA256 is used.
	Hash crypto.Hash

	// If nil, treat the update as a complete replacement for the contents of the file at TargetPath.
	// If non-nil, treat the update contents as a patch and use this object to apply the patch.
	Patcher Patcher

	// Store the old executable file at this path after a successful update.
	// The empty string means the old executable file will be removed after the update.
	OldSavePath string
}

// CheckPermissions determines whether the process has the correct permissions to
// perform the requested update. If the update can proceed, it returns nil, otherwise
// it returns the error that would occur if an update were attempted.
func (o *Options) CheckPermissions() error {
	// get the directory the file exists in
	path, err := o.getPath()
	if err != nil {
		return err
	}

	fileDir := filepath.Dir(path)
	fileName := filepath.Base(path)

	// attempt to open a file in the file's directory
	newPath := filepath.Join(fileDir, fmt.Sprintf(".%s.check-perm", fileName))
	fp, err := os.OpenFile(newPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, o.getMode())
	if err != nil {
		return err
	}
	fp.Close()

	_ = os.Remove(newPath)
	return nil
}

func (o *Options) getPath() (string, error) {
	if o.TargetPath == "" {
		return osext.Executable()
	} else {
		return o.TargetPath, nil
	}
}

func (o *Options) getMode() os.FileMode {
	if o.TargetMode == 0 {
		return 0755
	}
	return o.TargetMode
}

func (o *Options) getHash() crypto.Hash {
	if o.Hash == 0 {
		o.Hash = crypto.SHA256
	}
	return o.Hash
}

func (o *Options) applyPatch(patch io.Reader, targetPath string) ([]byte, error) {
	// open the file to patch
	old, err := os.Open(targetPath)
	if err != nil {
		return nil, err
	}
	defer old.Close()

	// apply the patch
	var applied bytes.Buffer
	if err = o.Patcher.Patch(old, &applied, patch); err != nil {
		return nil, err
	}

	return applied.Bytes(), nil
}

func (o *Options) verifyChecksum(updated []byte) error {
	checksum, err := checksumFor(o.getHash(), updated)
	if err != nil {
		return err
	}

	if !bytes.Equal(o.Checksum, checksum) {
		return fmt.Errorf("Updated file has wrong checksum. Expected: %x, got: %x", o.Checksum, checksum)
	}
	return nil
}

func checksumFor(h crypto.Hash, payload []byte) ([]byte, error) {
	if !h.Available() {
		return nil, errors.New("requested hash function not available")
	}
	hash := h.New()
	hash.Write(payload) // guaranteed not to error
	return hash.Sum([]byte{}), nil
}
