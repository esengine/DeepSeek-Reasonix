package workspacelist

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"reasonix/internal/base/filelock"
	"reasonix/internal/base/fileutil"
)

// FileName is where the list lives, at the top of the state root.
const FileName = "serve-workspaces.json"

// MaxPaths bounds the remembered list: it is the sidebar's tree, not a recents
// menu, so it holds more than a dropdown would.
const MaxPaths = 32

// AdoptedSuffix marks a list that has been merged into the current one, so the
// merge happens once.
const AdoptedSuffix = ".adopted"

// List is the remembered project order and the project a launch opens first.
type List struct {
	Paths  []string `json:"paths"`
	Launch string   `json:"launch,omitempty"`
}

// Read parses the list at path. A missing file or empty path is an empty list.
func Read(path string) (List, error) {
	var list List
	if path == "" {
		return list, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return list, nil
	}
	if err != nil {
		return list, err
	}
	data = bytes.TrimSpace(data)
	if len(data) > 0 && data[0] == '[' {
		err = json.Unmarshal(data, &list.Paths)
	} else {
		err = json.Unmarshal(data, &list)
	}
	if err != nil {
		return List{}, err
	}
	paths := make([]string, 0, len(list.Paths))
	for _, dir := range list.Paths {
		dir = strings.TrimSpace(dir)
		if dir != "" && !slices.Contains(paths, dir) {
			paths = append(paths, dir)
		}
	}
	list.Paths = paths
	if !slices.Contains(paths, list.Launch) {
		list.Launch = ""
		if len(paths) > 0 {
			list.Launch = paths[0]
		}
	}
	return list, nil
}

// Update applies mutate to the list under its lock and publishes the result
// atomically. With repair, a list that is not valid JSON is set aside as a
// backup and replaced rather than refusing every later write.
func Update(ctx context.Context, path string, repair bool, mutate func(*List) error) error {
	if path == "" {
		return errors.New("no workspace list directory")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	unlock, err := filelock.Acquire(ctx, path+".lock")
	if err != nil {
		return err
	}
	defer unlock()
	return updateLocked(path, repair, mutate)
}

func updateLocked(path string, repair bool, mutate func(*List) error) error {
	list, err := Read(path)
	if err != nil {
		var syntax *json.SyntaxError
		var shape *json.UnmarshalTypeError
		if !repair || (!errors.As(err, &syntax) && !errors.As(err, &shape)) {
			return err
		}
		if err := backup(path); err != nil {
			return err
		}
		list = List{}
	}
	if err := mutate(&list); err != nil {
		return err
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(path, data, 0o644)
}

func backup(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".bak-*")
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	return errors.Join(writeErr, f.Close())
}

// MergeOnce appends the projects of the list at old that the list at current
// lacks, then renames old aside so it is merged exactly once: a project the
// user forgets afterwards is not brought back by the next run. Current keeps
// its order and its launch project. It returns how many projects were added.
func MergeOnce(ctx context.Context, current, old string) (int, error) {
	if current == "" || old == "" {
		return 0, nil
	}
	if _, err := os.Stat(old); err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	incoming, err := Read(old)
	if err != nil {
		return 0, fmt.Errorf("workspace list %s: %w", old, err)
	}
	added := 0
	err = Update(ctx, current, false, func(list *List) error {
		for _, dir := range incoming.Paths {
			if len(list.Paths) >= MaxPaths {
				break
			}
			if !slices.ContainsFunc(list.Paths, func(have string) bool { return samePath(have, dir) }) {
				list.Paths = append(list.Paths, dir)
				added++
			}
		}
		if list.Launch == "" && len(list.Paths) > 0 {
			list.Launch = list.Paths[0]
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return added, fileutil.RenameAside(old, AdoptedSuffix)
}

func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
