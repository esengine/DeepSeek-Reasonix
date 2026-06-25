package snapshot

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"reasonix/internal/fileutil"
)

const DirName = "snapshots"

type SystemState struct {
	MemoryGraph      any `json:"memory_graph,omitempty"`
	ControlGraph     any `json:"control_graph,omitempty"`
	StrategyRegistry any `json:"strategy_registry,omitempty"`
	EquilibriumState any `json:"equilibrium_state,omitempty"`
}

type SystemSnapshot struct {
	ID               string          `json:"id"`
	CreatedAt        time.Time       `json:"created_at"`
	Stable           bool            `json:"stable"`
	ExecutionCount   int             `json:"execution_count,omitempty"`
	BarrierID        string          `json:"barrier_id,omitempty"`
	StateHash        string          `json:"state_hash,omitempty"`
	MemoryGraph      json.RawMessage `json:"memory_graph,omitempty"`
	ControlGraph     json.RawMessage `json:"control_graph,omitempty"`
	StrategyRegistry json.RawMessage `json:"strategy_registry,omitempty"`
	EquilibriumState json.RawMessage `json:"equilibrium_state,omitempty"`
}

type Barrier struct {
	ID       string    `json:"id"`
	FrozenAt time.Time `json:"frozen_at"`
}

func NewBarrier(id string, now time.Time) Barrier {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return Barrier{ID: sanitizeID(id), FrozenAt: now.UTC()}
}

func Capture(id string, state SystemState, stable bool, executionCount int, now time.Time) (SystemSnapshot, error) {
	return CaptureAtomic(id, state, stable, executionCount, NewBarrier(id, now))
}

func CaptureAtomic(id string, state SystemState, stable bool, executionCount int, barrier Barrier) (SystemSnapshot, error) {
	if strings.TrimSpace(id) == "" {
		return SystemSnapshot{}, fmt.Errorf("snapshot id is required")
	}
	if barrier.FrozenAt.IsZero() {
		barrier = NewBarrier(id, time.Now().UTC())
	}
	memoryGraph, err := marshalRaw(state.MemoryGraph)
	if err != nil {
		return SystemSnapshot{}, fmt.Errorf("marshal memory graph: %w", err)
	}
	controlGraph, err := marshalRaw(state.ControlGraph)
	if err != nil {
		return SystemSnapshot{}, fmt.Errorf("marshal control graph: %w", err)
	}
	strategyRegistry, err := marshalRaw(state.StrategyRegistry)
	if err != nil {
		return SystemSnapshot{}, fmt.Errorf("marshal strategy registry: %w", err)
	}
	equilibriumState, err := marshalRaw(state.EquilibriumState)
	if err != nil {
		return SystemSnapshot{}, fmt.Errorf("marshal equilibrium state: %w", err)
	}
	return SystemSnapshot{
		ID:               sanitizeID(id),
		CreatedAt:        barrier.FrozenAt.UTC(),
		Stable:           stable,
		ExecutionCount:   executionCount,
		BarrierID:        barrier.ID,
		StateHash:        stateHash(memoryGraph, controlGraph, strategyRegistry, equilibriumState),
		MemoryGraph:      memoryGraph,
		ControlGraph:     controlGraph,
		StrategyRegistry: strategyRegistry,
		EquilibriumState: equilibriumState,
	}, nil
}

func Save(root string, snap SystemSnapshot) error {
	if strings.TrimSpace(root) == "" {
		return fmt.Errorf("snapshot root is required")
	}
	if strings.TrimSpace(snap.ID) == "" {
		return fmt.Errorf("snapshot id is required")
	}
	path := filepath.Join(root, DirName, sanitizeID(snap.ID)+".json")
	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return fileutil.AtomicWriteFile(path, b, 0o600)
}

func Load(root, id string) (SystemSnapshot, error) {
	var snap SystemSnapshot
	b, err := os.ReadFile(filepath.Join(root, DirName, sanitizeID(id)+".json"))
	if err != nil {
		return snap, err
	}
	err = json.Unmarshal(b, &snap)
	return snap, err
}

func LatestStable(root string) (SystemSnapshot, error) {
	files, err := filepath.Glob(filepath.Join(root, DirName, "*.json"))
	if err != nil {
		return SystemSnapshot{}, err
	}
	sort.Strings(files)
	for i := len(files) - 1; i >= 0; i-- {
		var snap SystemSnapshot
		b, err := os.ReadFile(files[i])
		if err != nil {
			continue
		}
		if json.Unmarshal(b, &snap) == nil && snap.Stable {
			return snap, nil
		}
	}
	return SystemSnapshot{}, os.ErrNotExist
}

func RestoreRaw(root, id string) (SystemState, error) {
	snap, err := Load(root, id)
	if err != nil {
		return SystemState{}, err
	}
	return SystemState{
		MemoryGraph:      append(json.RawMessage(nil), snap.MemoryGraph...),
		ControlGraph:     append(json.RawMessage(nil), snap.ControlGraph...),
		StrategyRegistry: append(json.RawMessage(nil), snap.StrategyRegistry...),
		EquilibriumState: append(json.RawMessage(nil), snap.EquilibriumState...),
	}, nil
}

func marshalRaw(v any) (json.RawMessage, error) {
	if v == nil {
		return nil, nil
	}
	if raw, ok := v.(json.RawMessage); ok {
		return append(json.RawMessage(nil), raw...), nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

func stateHash(parts ...json.RawMessage) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(part)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func sanitizeID(id string) string {
	id = strings.TrimSpace(id)
	id = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-' || r == '_' || r == '.':
			return r
		default:
			return '-'
		}
	}, id)
	if id == "" {
		return "snapshot"
	}
	return id
}
