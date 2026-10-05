package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/base/fileutil"
)

const yoloAckFilename = "yolo-acknowledged.json"

// YoloAcknowledged reports whether the person already confirmed, in an
// interactive session, that YOLO skips approval prompts. The record lives only
// under their Reasonix home: no project file and no flag can stand in for it.
func YoloAcknowledged(home string) bool {
	if strings.TrimSpace(home) == "" {
		return false
	}
	data, err := os.ReadFile(filepath.Join(home, yoloAckFilename))
	if err != nil {
		return false
	}
	var rec struct {
		AcknowledgedAt time.Time `json:"acknowledged_at"`
	}
	return json.Unmarshal(data, &rec) == nil && !rec.AcknowledgedAt.IsZero()
}

// AcknowledgeYolo records that confirmation.
func AcknowledgeYolo(home string) error {
	if strings.TrimSpace(home) == "" {
		return errors.New("no Reasonix home to record the YOLO confirmation in")
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(map[string]time.Time{"acknowledged_at": time.Now().UTC()})
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(filepath.Join(home, yoloAckFilename), append(data, '\n'), 0o600)
}
