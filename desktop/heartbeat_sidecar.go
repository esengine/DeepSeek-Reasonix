package main

import (
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"

	"reasonix/internal/config"
	"reasonix/internal/fileutil"
)

// The top-level runs field remains readable by the previous sidecar reader;
// current readers use revision/previous to make the two-file commit consistent.
const heartbeatRunHistorySchemaVersion = 1

type heartbeatRunHistoryGeneration struct {
	Revision uint64                    `json:"revision"`
	Runs     map[string][]HeartbeatRun `json:"runs"`
}

type heartbeatRunHistorySidecar struct {
	SchemaVersion int                            `json:"schemaVersion,omitempty"`
	Revision      uint64                         `json:"revision,omitempty"`
	Runs          map[string][]HeartbeatRun      `json:"runs"`
	Previous      *heartbeatRunHistoryGeneration `json:"previous,omitempty"`
}

func (e *HeartbeatEngine) runHistoryPath() string {
	dir := config.MemoryUserDir()
	if dir == "" {
		dir = "."
	}
	return filepath.Join(dir, "heartbeat-tasks.runs.json")
}

// The previous generation keeps the old config readable if the process exits
// after publishing a new sidecar but before publishing the matching config.
func (e *HeartbeatEngine) readRunHistorySidecar(cfg heartbeatConfig) (map[string][]HeartbeatRun, error) {
	b, err := readFileUTF8(e.runHistoryPath())
	if err != nil {
		return nil, nil
	}
	var sidecar heartbeatRunHistorySidecar
	if err := json.Unmarshal(b, &sidecar); err != nil {
		log.Printf("[heartbeat] invalid run-history sidecar: %v", err)
		return nil, nil
	}
	if sidecar.SchemaVersion > heartbeatRunHistorySchemaVersion {
		return nil, fmt.Errorf("heartbeat run-history sidecar schemaVersion %d is newer than this binary supports (%d); upgrade Reasonix", sidecar.SchemaVersion, heartbeatRunHistorySchemaVersion)
	}
	if sidecar.SchemaVersion == 0 && sidecar.Revision == 0 && sidecar.Previous == nil {
		return sidecar.Runs, nil
	}
	if sidecar.Revision == cfg.Revision {
		return sidecar.Runs, nil
	}
	if sidecar.Previous != nil && sidecar.Previous.Revision == cfg.Revision {
		return sidecar.Previous.Runs, nil
	}
	// An older writer can replace only the main config and omit Revision.
	if cfg.Revision == 0 || cfg.Revision > sidecar.Revision {
		return sidecar.Runs, nil
	}
	log.Printf("[heartbeat] run-history sidecar revision %d does not match config revision %d; ignoring staged generation", sidecar.Revision, cfg.Revision)
	return nil, nil
}

func (e *HeartbeatEngine) writeRunHistorySidecar(revision uint64, runs map[string][]HeartbeatRun, previous *heartbeatRunHistoryGeneration) error {
	b, err := json.MarshalIndent(heartbeatRunHistorySidecar{
		SchemaVersion: heartbeatRunHistorySchemaVersion,
		Revision:      revision,
		Runs:          runs,
		Previous:      previous,
	}, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(e.runHistoryPath(), b, 0o644)
}

func heartbeatRunHistoryByTask(tasks []HeartbeatTask) map[string][]HeartbeatRun {
	runs := make(map[string][]HeartbeatRun, len(tasks))
	for _, task := range tasks {
		if len(task.RunHistory) > 0 {
			runs[task.ID] = task.RunHistory
		}
	}
	return runs
}
