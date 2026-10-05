package update

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// treeHandoffArg makes a copied host binary the process that swaps a staged
// tree in once the application has exited. Every host's main checks it first.
const treeHandoffArg = "--reasonix-tree-handoff"

// ErrStagedTreeChanged is a staged file that no longer hashes to what the
// release says it is, found at the last moment before it would be installed.
var ErrStagedTreeChanged = errors.New("update: a staged file changed before it was installed")

// ErrTreeNotSwappable is an install the swap could not move a file out of:
// one this user cannot write, or one on another volume than its backup.
var ErrTreeNotSwappable = errors.New("update: the swap cannot move files out of this install")

// TreeHandoff is everything the swap needs once the application is gone. It is
// written beside the staged tree and read back by the copied helper, so the
// helper trusts nothing it was not told in this one file.
type TreeHandoff struct {
	Version    string       `json:"version"`
	InstallDir string       `json:"installDir"`
	StagingDir string       `json:"stagingDir"`
	BackupDir  string       `json:"backupDir"`
	Files      []StagedFile `json:"files"`
	Relaunch   string       `json:"relaunch"`
	WaitPIDs   []int        `json:"waitPids"`
	// Outcome is where a failed swap leaves its reason. The helper is detached
	// and has no one to tell, so the build it restores reads it on its next launch.
	Outcome string `json:"outcome,omitempty"`
}

// StagedFile is one file of the new tree, slash-separated and relative.
type StagedFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// SwapBackupDirName is where, inside an install, a swap sets aside the files
// it replaces. Inside the install is the one place certain to share its volume,
// which a rename needs; the update cache may sit on another drive.
const SwapBackupDirName = ".reasonix-update"

// SwapBackupDir is the directory a swap of installDir backs up into.
func SwapBackupDir(installDir string) string { return filepath.Join(installDir, SwapBackupDirName) }

// renameFile is os.Rename, indirected so a test can put two directories on
// different volumes without a second drive.
var renameFile = os.Rename

// mkdirAll is os.MkdirAll, indirected so a test can refuse a directory the way
// an all-users install refuses a standard user.
var mkdirAll = os.MkdirAll

// fsRetries covers the seconds after exit in which Windows and its scanners
// still hold a file the application just closed.
var fsRetries = 60

const fsRetryDelay = 250 * time.Millisecond

// ApplyTree moves every staged file into the install, keeping what it replaces
// in BackupDir. Staged files are hashed again first, so the tree installed is
// the tree the release describes. Any failure restores every file already
// moved; files the release does not list are left where they are.
func ApplyTree(h TreeHandoff) error {
	for _, f := range h.Files {
		if err := checkStaged(filepath.Join(h.StagingDir, filepath.FromSlash(f.Path)), f.SHA256); err != nil {
			return fmt.Errorf("%w: %s", err, f.Path)
		}
	}
	type moved struct{ dst, bak string }
	var done []moved
	rollback := func() {
		for _, m := range slices.Backward(done) {
			_ = retry(func() error { return removeIfPresent(m.dst) })
			if m.bak != "" {
				_ = retry(func() error { return renameFile(m.bak, m.dst) })
			}
		}
	}
	for _, f := range h.Files {
		rel := filepath.FromSlash(f.Path)
		src, dst := filepath.Join(h.StagingDir, rel), filepath.Join(h.InstallDir, rel)
		m := moved{dst: dst}
		if _, err := os.Lstat(dst); err == nil {
			m.bak = filepath.Join(h.BackupDir, rel)
			if err := os.MkdirAll(filepath.Dir(m.bak), 0o755); err != nil {
				rollback()
				return err
			}
			if err := retry(func() error { return renameFile(dst, m.bak) }); err != nil {
				rollback()
				return fmt.Errorf("update: set aside %s: %w", f.Path, err)
			}
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			done = append(done, m)
			rollback()
			return err
		}
		if err := retry(func() error { return moveFile(src, dst) }); err != nil {
			done = append(done, m)
			rollback()
			return fmt.Errorf("update: install %s: %w", f.Path, err)
		}
		done = append(done, m)
	}
	return nil
}

// CheckTreeSwap proves, before anything is fetched, the one move every swap
// starts with: a new entry in installDir renamed into backupParent. Rename
// never crosses a volume, and an all-users install grants its users no writes,
// so either would only reach ApplyTree to be rolled back.
func CheckTreeSwap(installDir, backupParent string) error {
	if err := mkdirAll(backupParent, 0o755); err != nil {
		return fmt.Errorf("%w: %w", ErrTreeNotSwappable, err)
	}
	probe, err := os.CreateTemp(installDir, ".reasonix-swap-probe-*")
	if err != nil {
		return fmt.Errorf("%w: %w", ErrTreeNotSwappable, err)
	}
	name := probe.Name()
	_ = probe.Close()
	aside := filepath.Join(backupParent, filepath.Base(name))
	if err := renameFile(name, aside); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("%w: %w", ErrTreeNotSwappable, err)
	}
	return os.Remove(aside)
}

func checkStaged(path, want string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != want {
		return ErrStagedTreeChanged
	}
	return nil
}

// moveFile renames, and where the staging and install directories are on
// different volumes copies to a temporary name beside dst and renames that, so
// dst is never seen half-written.
func moveFile(src, dst string) error {
	if err := renameFile(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".reasonix-part-*")
	if err != nil {
		return err
	}
	tmp := out.Name()
	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp, st.Mode().Perm())
	}
	if err == nil {
		err = renameFile(tmp, dst)
	}
	if err != nil {
		_ = os.Remove(tmp)
	}
	return err
}

func removeIfPresent(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func retry(op func() error) error {
	var err error
	for range fsRetries {
		if err = op(); err == nil {
			return nil
		}
		time.Sleep(fsRetryDelay)
	}
	return err
}

// WriteTreeHandoff records h beside its staged tree and returns where.
func WriteTreeHandoff(h TreeHandoff) (string, error) {
	b, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return "", err
	}
	path := strings.TrimRight(h.StagingDir, `\/`) + ".handoff.json"
	return path, os.WriteFile(path, b, 0o600)
}

func readTreeHandoff(path string) (TreeHandoff, error) {
	var h TreeHandoff
	b, err := os.ReadFile(path)
	if err != nil {
		return h, err
	}
	if err := json.Unmarshal(b, &h); err != nil {
		return h, err
	}
	if h.InstallDir == "" || h.StagingDir == "" || h.BackupDir == "" || len(h.Files) == 0 {
		return h, fmt.Errorf("update: tree handoff %s is incomplete", path)
	}
	return h, nil
}

// MaybeRunTreeHandoff runs the swap when this process was started as the
// helper for one. It waits for the application to exit, installs the staged
// tree or restores the old one, and starts whichever is now installed.
func MaybeRunTreeHandoff(args []string) (handled bool, exitCode int) {
	if len(args) != 2 || args[0] != treeHandoffArg {
		return false, 0
	}
	h, err := readTreeHandoff(args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "tree handoff:", err)
		return true, 2
	}
	waitForExit(h.WaitPIDs)
	code := 0
	err = ApplyTree(h)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tree handoff:", err)
		code = 1
		if h.Outcome != "" {
			_ = os.WriteFile(h.Outcome, []byte(err.Error()), 0o600)
		}
	} else {
		afterTreeInstalled(h)
	}
	if err := relaunch(h.Relaunch); err != nil {
		fmt.Fprintln(os.Stderr, "tree handoff: relaunch:", err)
	}
	// After the relaunch, so a directory a scanner still holds delays nobody.
	if code == 0 {
		_ = retry(func() error { return os.RemoveAll(h.BackupDir) })
		_ = retry(func() error { return os.RemoveAll(h.StagingDir) })
		if parent := filepath.Dir(h.BackupDir); filepath.Base(parent) == SwapBackupDirName {
			_ = os.Remove(parent) // only once empty; another release's backup keeps it
		}
	}
	return true, code
}
