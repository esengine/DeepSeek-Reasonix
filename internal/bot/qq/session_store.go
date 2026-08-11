package qq

// The QQ gateway session is deliberately kept outside the TOML configuration.
// It is runtime state, not a credential, and keeping it in a small sidecar lets
// a restart resume without rewriting user configuration or exposing secrets.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"reasonix/internal/config"
)

const qqGatewayStateMaxAge = 24 * time.Hour

type qqGatewayState struct {
	SessionID string    `json:"session_id"`
	Seq       int64     `json:"seq"`
	AppID     string    `json:"app_id"`
	SavedAt   time.Time `json:"saved_at"`
}

func saveQQGatewayRawEvent(appID string, sandbox bool, event gatewayPayload) error {
	base := strings.TrimSpace(config.UserConfigPath())
	if base == "" {
		return nil
	}
	dir := filepath.Join(filepath.Dir(base), "qq-gateway-events")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	identity := gatewayPayloadEventID(event)
	if identity == "" {
		identity = strings.TrimSpace(event.T)
	}
	if identity == strings.TrimSpace(event.T) && event.S != 0 {
		identity = fmt.Sprintf("%s:%d", identity, event.S)
	}
	if identity == "" {
		identity = string(event.D)
	}
	sum := sha256.Sum256([]byte(appID + ":" + identity))
	path := filepath.Join(dir, hex.EncodeToString(sum[:])[:32]+".json")
	data, err := json.Marshal(persistedGatewayEvent{AppID: strings.TrimSpace(appID), Sandbox: sandbox, Payload: event})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".event-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func gatewayPayloadEventID(event gatewayPayload) string {
	var body struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(event.D, &body) != nil {
		return ""
	}
	return strings.TrimSpace(body.ID)
}

func removeQQGatewayRawEvent(appID string, sandbox bool, eventID string) error {
	eventID = strings.TrimSpace(eventID)
	base := strings.TrimSpace(config.UserConfigPath())
	if base == "" || eventID == "" {
		return nil
	}
	dir := filepath.Join(filepath.Dir(base), "qq-gateway-events")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		var saved persistedGatewayEvent
		if json.Unmarshal(data, &saved) != nil || strings.TrimSpace(saved.AppID) != strings.TrimSpace(appID) || saved.Sandbox != sandbox {
			continue
		}
		if gatewayPayloadEventID(saved.Payload) != eventID {
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

type persistedGatewayEvent struct {
	AppID   string         `json:"app_id"`
	Sandbox bool           `json:"sandbox"`
	Payload gatewayPayload `json:"payload"`
}

// loadQQGatewayRawEvents returns only events written by this QQ application.
// Older direct-payload files are intentionally ignored because they lack an
// app identity and could otherwise be replayed into the wrong account.
func loadQQGatewayRawEvents(appID string, sandbox bool) ([]gatewayPayload, error) {
	base := strings.TrimSpace(config.UserConfigPath())
	if base == "" {
		return nil, nil
	}
	dir := filepath.Join(filepath.Dir(base), "qq-gateway-events")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []gatewayPayload
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(dir, entry.Name()))
		if readErr != nil {
			return nil, readErr
		}
		var saved persistedGatewayEvent
		if json.Unmarshal(data, &saved) != nil || strings.TrimSpace(saved.AppID) != strings.TrimSpace(appID) || saved.Sandbox != sandbox {
			continue
		}
		if saved.Payload.T == "" || len(saved.Payload.D) == 0 {
			continue
		}
		out = append(out, saved.Payload)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].S != out[j].S {
			return out[i].S < out[j].S
		}
		return out[i].T < out[j].T
	})
	return out, nil
}

func qqGatewayStatePath(appID string, sandbox bool) string {
	base := strings.TrimSpace(config.UserConfigPath())
	if base == "" {
		return ""
	}
	name := "qq-gateway-state-"
	if sandbox {
		name += "sandbox-"
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(appID)))
	name += hex.EncodeToString(sum[:])[:16] + ".json"
	return filepath.Join(filepath.Dir(base), name)
}

func loadQQGatewayState(appID string, sandbox bool) (qqGatewayState, error) {
	path := qqGatewayStatePath(appID, sandbox)
	if path == "" {
		return qqGatewayState{}, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return qqGatewayState{}, nil
	}
	if err != nil {
		return qqGatewayState{}, err
	}
	var state qqGatewayState
	if err := json.Unmarshal(data, &state); err != nil {
		return qqGatewayState{}, fmt.Errorf("decode qq gateway state: %w", err)
	}
	if strings.TrimSpace(state.SessionID) == "" || strings.TrimSpace(state.AppID) != strings.TrimSpace(appID) || state.SavedAt.IsZero() || time.Since(state.SavedAt) > qqGatewayStateMaxAge {
		return qqGatewayState{}, nil
	}
	return state, nil
}

func saveQQGatewayState(appID string, sandbox bool, state qqGatewayState) error {
	path := qqGatewayStatePath(appID, sandbox)
	if path == "" || strings.TrimSpace(state.SessionID) == "" {
		return nil
	}
	state.AppID = strings.TrimSpace(appID)
	state.SavedAt = time.Now().UTC()
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".qq-gateway-state-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func clearQQGatewayState(appID string, sandbox bool) error {
	path := qqGatewayStatePath(appID, sandbox)
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
