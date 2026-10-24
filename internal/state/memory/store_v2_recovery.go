package memory

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	retiredPrefix = ".retired-"
	stagedPrefix  = ".staged-"
)

// publishArtifact is one hidden copy publishRename leaves behind: the retired
// file under the old name, or the staged file under the new one.
type publishArtifact struct {
	dir    string
	file   string
	name   string // slug embedded in the hidden name
	memory Memory
	loaded bool
	stamp  int64
	staged bool
}

// recoverInterruptedRename repairs a rename killed after the old file was
// retired. A staged copy carrying id pairs by stamp with the retired copy of
// the same publish call; when the retired bytes are readable they are restored
// under the old name, so the caller's expected revision still names it.
func (s Store) recoverInterruptedRename(id string) (Memory, string, bool, error) {
	staged, retired, err := s.publishArtifacts()
	if err != nil {
		return Memory{}, "", false, err
	}
	var found bool
	var pickStaged, pickRetired publishArtifact
	for _, artifact := range staged {
		if artifact.memory.ID != id {
			continue
		}
		old, ok := retired[artifact.stamp]
		if !ok || (found && artifact.stamp <= pickStaged.stamp) {
			continue
		}
		found, pickStaged, pickRetired = true, artifact, old
	}
	if !found {
		return Memory{}, "", false, nil
	}
	if pickRetired.loaded {
		return s.rollBackPublish(id, pickRetired, pickStaged)
	}
	return s.finishPublish(id, pickStaged, pickRetired)
}

// publishArtifacts scans every store directory for interrupted-publish copies:
// staged copies keyed by their parsed id, and retired copies keyed by stamp.
func (s Store) publishArtifacts() ([]publishArtifact, map[int64]publishArtifact, error) {
	var staged []publishArtifact
	retired := map[int64]publishArtifact{}
	for _, dir := range s.dirs() {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		entries, err := readMemoryDirectory(dir)
		if err != nil {
			return nil, nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			hidden := entry.Name()
			isStaged := strings.HasPrefix(hidden, stagedPrefix)
			prefix := retiredPrefix
			if isStaged {
				prefix = stagedPrefix
			} else if !strings.HasPrefix(hidden, retiredPrefix) {
				continue
			}
			factName, stamp, ok := parseArtifactName(hidden, prefix)
			if !ok {
				continue
			}
			path := filepath.Join(dir, hidden)
			// An artifact that cannot be read cannot be matched by id or name
			// either, so it stays unloaded here; the artifact name still lets a
			// later pass retry it.
			memory, loaded, _ := loadMemory(path)
			artifact := publishArtifact{
				dir: dir, file: hidden, name: factName, memory: memory,
				loaded: loaded, stamp: stamp, staged: isStaged,
			}
			if isStaged {
				if loaded {
					staged = append(staged, artifact)
				}
				continue
			}
			if _, taken := retired[stamp]; !taken {
				retired[stamp] = artifact
			}
		}
	}
	return staged, retired, nil
}

// parseArtifactName splits ".retired-<name>-<stamp>" into its name and stamp.
func parseArtifactName(hidden, prefix string) (string, int64, bool) {
	stem, ok := strings.CutPrefix(hidden, prefix)
	if !ok {
		return "", 0, false
	}
	i := strings.LastIndexByte(stem, '-')
	if i <= 0 {
		return "", 0, false
	}
	stamp, err := strconv.ParseInt(stem[i+1:], 10, 64)
	if err != nil || stamp <= 0 {
		return "", 0, false
	}
	return stem[:i], stamp, true
}

// rollBackPublish restores the retired bytes under the old name and drops the
// staged copy; the retry that found the id absent then repeats the rename.
func (s Store) rollBackPublish(id string, retired, staged publishArtifact) (Memory, string, bool, error) {
	source := filepath.Join(retired.dir, retired.file)
	raw, err := os.ReadFile(source)
	if err != nil {
		return Memory{}, "", false, err
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(source); err == nil {
		mode = info.Mode().Perm()
	}
	name := slug(retired.name)
	path, err := safeJoin(retired.dir, name+".md")
	if err != nil {
		return Memory{}, "", false, err
	}
	if err := writeMemoryCreate(path, raw, mode); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return Memory{}, "", false, fmt.Errorf("interrupted rename for memory id %q: name %q is now active", id, name)
		}
		return Memory{}, "", false, err
	}
	dropPublishArtifacts(retired, staged)
	restored := retired.memory
	restored.ID, restored.Name = id, name
	if restored.Scope == "" {
		restored.Scope = s.scopeForDir(retired.dir)
	}
	if err := reindexIn(retired.dir, name, restored); err != nil {
		return Memory{}, "", false, err
	}
	return restored, path, true, nil
}

// finishPublish publishes the staged copy forward when the retired bytes cannot
// be read; the fact becomes readable under the name the staged file carries.
func (s Store) finishPublish(id string, staged, retired publishArtifact) (Memory, string, bool, error) {
	name := slug(staged.name)
	path, err := safeJoin(staged.dir, name+".md")
	if err != nil {
		return Memory{}, "", false, err
	}
	if active, _, ok, err := s.findActiveInDir(staged.dir, name); err != nil {
		return Memory{}, "", false, err
	} else if ok && active.ID != id {
		return Memory{}, "", false, fmt.Errorf("interrupted rename for memory id %q: name %q is now active as id %q", id, name, active.ID)
	}
	root, err := os.OpenRoot(staged.dir)
	if err != nil {
		return Memory{}, "", false, err
	}
	defer root.Close()
	if err := renameMemoryFile(root, staged.file, filepath.Base(path)); err != nil {
		return Memory{}, "", false, err
	}
	dropPublishArtifacts(staged, retired)
	restored := staged.memory
	restored.ID, restored.Name = id, name
	if restored.Scope == "" {
		restored.Scope = s.scopeForDir(staged.dir)
	}
	if err := reindexIn(staged.dir, name, restored); err != nil {
		return Memory{}, "", false, err
	}
	return restored, path, true, nil
}

func dropPublishArtifacts(artifacts ...publishArtifact) {
	for _, artifact := range artifacts {
		_ = os.Remove(filepath.Join(artifact.dir, artifact.file))
	}
}
