package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"reasonix/internal/filelock"
	"reasonix/internal/fileutil"
)

// oauthRefreshGates prevent duplicate refresh requests from transports in the
// same Reasonix process. The file lock below remains the cross-process source
// of truth, but it must not be held across the token endpoint network request.
var oauthRefreshGates sync.Map // map[string]chan struct{}

func mcpOAuthStatePath(stateDir string) string {
	if strings.TrimSpace(stateDir) == "" {
		return ""
	}
	return filepath.Join(stateDir, mcpOAuthStateFile)
}

func mcpOAuthGenerationPath(stateDir string) string {
	if strings.TrimSpace(stateDir) == "" {
		return ""
	}
	return filepath.Join(stateDir, mcpOAuthGenerationFile)
}

func acquireMCPOAuthStateLock(ctx context.Context, stateDir string) (func(), error) {
	path := mcpOAuthStatePath(stateDir)
	if path == "" {
		return nil, fmt.Errorf("private state directory is unavailable")
	}
	return filelock.Acquire(ctx, path+".lock")
}

func acquireMCPOAuthRefreshGate(ctx context.Context, stateDir string) (func(), error) {
	key := filepath.Clean(strings.TrimSpace(stateDir))
	if key == "." || key == "" {
		return nil, fmt.Errorf("private state directory is unavailable")
	}
	gate, _ := oauthRefreshGates.LoadOrStore(key, make(chan struct{}, 1))
	select {
	case gate.(chan struct{}) <- struct{}{}:
		return func() { <-gate.(chan struct{}) }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func loadMCPOAuthState(stateDir string) (mcpOAuthState, error) {
	path := mcpOAuthStatePath(stateDir)
	if path == "" {
		return mcpOAuthState{}, nil
	}
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return mcpOAuthState{}, nil
		}
		return mcpOAuthState{}, fmt.Errorf("read MCP OAuth state: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return mcpOAuthState{}, fmt.Errorf("read MCP OAuth state: refusing non-regular file")
	}
	if info.Size() > maxOAuthBody {
		return mcpOAuthState{}, fmt.Errorf("read MCP OAuth state: file is too large")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return mcpOAuthState{}, fmt.Errorf("read MCP OAuth state: %w", err)
	}
	var state mcpOAuthState
	if err := json.Unmarshal(b, &state); err != nil {
		return mcpOAuthState{}, fmt.Errorf("decode MCP OAuth state: %w", err)
	}
	if state.Version != 1 {
		return mcpOAuthState{}, fmt.Errorf("decode MCP OAuth state: unsupported version %d", state.Version)
	}
	return state, nil
}

func saveMCPOAuthState(stateDir string, state mcpOAuthState) error {
	path := mcpOAuthStatePath(stateDir)
	if path == "" {
		return fmt.Errorf("save MCP OAuth state: private state directory is unavailable")
	}
	if info, err := os.Lstat(path); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
		return fmt.Errorf("save MCP OAuth state: refusing non-regular file")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("save MCP OAuth state: %w", err)
	}
	state.Version = 1
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode MCP OAuth state: %w", err)
	}
	if err := fileutil.AtomicWriteFileStrict(path, append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("save MCP OAuth state: %w", err)
	}
	return nil
}

func loadMCPOAuthGeneration(stateDir string) (string, error) {
	path := mcpOAuthGenerationPath(stateDir)
	if path == "" {
		return "", nil
	}
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("read MCP OAuth generation: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", fmt.Errorf("read MCP OAuth generation: refusing non-regular file")
	}
	if info.Size() > 256 {
		return "", fmt.Errorf("read MCP OAuth generation: file is too large")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read MCP OAuth generation: %w", err)
	}
	generation := strings.TrimSpace(string(b))
	if generation == "" {
		return "", fmt.Errorf("read MCP OAuth generation: empty generation")
	}
	return generation, nil
}

func saveMCPOAuthGeneration(stateDir, generation string) error {
	path := mcpOAuthGenerationPath(stateDir)
	if path == "" {
		return fmt.Errorf("save MCP OAuth generation: private state directory is unavailable")
	}
	if strings.TrimSpace(generation) == "" {
		return fmt.Errorf("save MCP OAuth generation: generation is empty")
	}
	if info, err := os.Lstat(path); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
		return fmt.Errorf("save MCP OAuth generation: refusing non-regular file")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("save MCP OAuth generation: %w", err)
	}
	if err := fileutil.AtomicWriteFileStrict(path, []byte(strings.TrimSpace(generation)+"\n"), 0o600); err != nil {
		return fmt.Errorf("save MCP OAuth generation: %w", err)
	}
	return nil
}

func bumpMCPOAuthGeneration(stateDir string) error {
	generation, err := randomBase64URL(24)
	if err != nil {
		return fmt.Errorf("create MCP OAuth generation: %w", err)
	}
	return saveMCPOAuthGeneration(stateDir, generation)
}

func captureMCPOAuthGeneration(ctx context.Context, stateDir string) (string, error) {
	release, err := acquireMCPOAuthStateLock(ctx, stateDir)
	if err != nil {
		return "", fmt.Errorf("lock MCP OAuth generation: %w", err)
	}
	defer release()
	return loadMCPOAuthGeneration(stateDir)
}

func saveMCPOAuthStateIfGenerationUnchanged(ctx context.Context, stateDir, generation string, state mcpOAuthState) error {
	release, err := acquireMCPOAuthStateLock(ctx, stateDir)
	if err != nil {
		return fmt.Errorf("lock MCP OAuth state: %w", err)
	}
	defer release()
	current, err := loadMCPOAuthGeneration(stateDir)
	if err != nil {
		return err
	}
	if current != generation {
		return fmt.Errorf("MCP OAuth authorization was invalidated while waiting for the browser; authorize again")
	}
	return saveMCPOAuthState(stateDir, state)
}
