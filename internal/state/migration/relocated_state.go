package migration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/state/workspacelist"
)

const adoptedSuffix = workspacelist.AdoptedSuffix

// AdoptRelocatedStateEntries brings across what an earlier relocation left in
// the home root: the wallpaper, the remembered project list, standing
// instructions, memory and the like. Each entry is merged once and then set
// aside as <name>.adopted, so a file the user deletes afterwards is not
// brought back by the next run. Nothing already in the new root is overwritten.
func AdoptRelocatedStateEntries(sink event.Sink) []string {
	// Same rule the automatic importers follow: a run that redirected its roots
	// for this process did not ask for the production install to be moved into
	// it. A relocation lives in config and is the user's standing choice.
	if config.IsolatedHomeDir() != "" || config.IsolatedStateDir() != "" {
		return nil
	}
	state, home := config.MemoryUserDir(), config.ReasonixHomeDir()
	if state == "" || home == "" || samePath(state, home) {
		return nil
	}
	var adopted []string
	for _, name := range config.StateRootEntriesEarlyMovesLeft {
		src, dst := filepath.Join(home, name), filepath.Join(state, name)
		info, err := os.Lstat(src)
		if err != nil || info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			// A link or junction points at data this run does not own; copying
			// nothing and renaming it would disable that data.
			continue
		}
		n, err := adoptEntry(name, src, dst)
		if err != nil {
			sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn,
				Text: fmt.Sprintf("could not bring %s across from the previous storage location: %v", name, err)})
			continue
		}
		if n > 0 {
			adopted = append(adopted, name)
		}
	}
	if len(adopted) > 0 {
		sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
			Text: fmt.Sprintf("brought %v across from the previous storage location", adopted)})
	}
	return adopted
}

func adoptEntry(name, src, dst string) (int, error) {
	if name == workspacelist.FileName {
		return workspacelist.MergeOnce(context.Background(), dst, src)
	}
	n, err := copyMissingTree(src, dst)
	if err != nil {
		return 0, err
	}
	// A file the new root already has is that install's own and stays; the old
	// one is left where it is rather than renamed over a decision. A directory
	// is set aside only once its contents are accounted for.
	if n == 0 && !isEmptyDir(src) {
		return 0, nil
	}
	return n, fileutil.RenameAside(src, adoptedSuffix)
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func isEmptyDir(path string) bool {
	entries, err := os.ReadDir(path)
	return err == nil && len(entries) == 0
}
