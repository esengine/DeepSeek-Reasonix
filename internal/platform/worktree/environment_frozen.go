package worktree

import (
	"context"
	"errors"
	"os"
	"reflect"
)

// FrozenEnvironment owns a distinct copy of declared preparation inputs.
// Restoring it never reads the original running workspace again.
type FrozenEnvironment struct {
	Version int                `json:"version"`
	Root    string             `json:"root"`
	Receipt EnvironmentReceipt `json:"receipt"`
}

func FreezeEnvironment(ctx context.Context, snap Snapshot, storage string, paths []string, denied ...string) (*FrozenEnvironment, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	if err := os.Mkdir(storage, 0700); err != nil {
		return nil, err
	}
	receipt, err := PrepareEnvironment(ctx, snap, Candidate{WorktreeRoot: storage}, paths, denied...)
	if err != nil {
		return nil, err
	}
	return &FrozenEnvironment{Version: 1, Root: storage, Receipt: *receipt}, nil
}

func RestoreFrozenEnvironment(ctx context.Context, frozen FrozenEnvironment, candidate Candidate) (*EnvironmentReceipt, error) {
	if frozen.Version != 1 || frozen.Root == "" || frozen.Receipt.Version != 1 || len(frozen.Receipt.Paths) == 0 {
		return nil, ErrEnvironment
	}
	receipt, err := PrepareEnvironment(ctx, Snapshot{RepoRoot: frozen.Root}, candidate, frozen.Receipt.Paths)
	if err != nil || !reflect.DeepEqual(receipt, &frozen.Receipt) {
		return nil, errors.Join(ErrEnvironment, err)
	}
	return receipt, nil
}

func CloneFrozenEnvironment(value *FrozenEnvironment) *FrozenEnvironment {
	if value == nil {
		return nil
	}
	copy := *value
	copy.Receipt.Paths = append([]string(nil), value.Receipt.Paths...)
	return &copy
}
