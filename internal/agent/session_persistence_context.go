package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"reasonix/internal/fileutil"
	"reasonix/internal/provider"
)

const sessionContextWriteChunk = 256 << 10

type jsonMarshalResult struct {
	data []byte
	err  error
}

type sessionPublishStartHookKey struct{}

func withSessionPublishStartHook(ctx context.Context, hook func()) context.Context {
	return context.WithValue(ctx, sessionPublishStartHookKey{}, hook)
}

// marshalJSONContext lets maintenance work release session locks promptly
// when a large attachment is still being encoded. Foreground saves keep the
// synchronous path through their background context wrappers.
func marshalJSONContext(ctx context.Context, value any) ([]byte, error) {
	return marshalJSONWithIndentContext(ctx, value, false)
}

func marshalJSONIndentContext(ctx context.Context, value any) ([]byte, error) {
	return marshalJSONWithIndentContext(ctx, value, true)
}

func marshalJSONWithIndentContext(ctx context.Context, value any, indent bool) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	marshal := func() ([]byte, error) {
		if indent {
			return json.MarshalIndent(value, "", "  ")
		}
		return json.Marshal(value)
	}
	if ctx.Done() == nil {
		return marshal()
	}
	resultCh := make(chan jsonMarshalResult, 1)
	go func() {
		data, err := marshal()
		resultCh <- jsonMarshalResult{data: data, err: err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-resultCh:
		return result.data, result.err
	}
}

func atomicWriteFileContext(ctx context.Context, path, pattern, crashOp string, data []byte, perm os.FileMode, syncFile bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if crashOp != "" {
		fileutil.Crash(crashOp, path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), pattern)
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	if err := tmp.Chmod(perm); err != nil {
		cleanup()
		return err
	}
	if err := writeContextBytes(ctx, tmp, path, data); err != nil {
		cleanup()
		return err
	}
	if err := ctx.Err(); err != nil {
		cleanup()
		return err
	}
	if syncFile {
		if err := tmp.Sync(); err != nil {
			cleanup()
			return err
		}
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := ctx.Err(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := fileutil.ReplaceFile(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func writeContextBytes(ctx context.Context, file *os.File, path string, data []byte) error {
	for len(data) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		chunk := min(len(data), sessionContextWriteChunk)
		written, writeErr := file.Write(data[:chunk])
		if writeErr != nil {
			return writeErr
		}
		if written == 0 {
			return fmt.Errorf("write %s: no progress", path)
		}
		data = data[written:]
	}
	return ctx.Err()
}

func writeSessionMessages(path string, msgs []provider.Message) error {
	return writeSessionMessagesContext(context.Background(), path, msgs)
}

func writeSessionMessagesContext(ctx context.Context, path string, msgs []provider.Message) error {
	// The compatibility transcript is a crash-safe anchor when the event log
	// is damaged. Maintenance checks cancellation before every publish step.
	if err := ctx.Err(); err != nil {
		return err
	}
	fileutil.Crash("session-checkpoint", path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".session.*.tmp")
	if err != nil {
		return fmt.Errorf("create session tmp: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	if hook, _ := ctx.Value(sessionPublishStartHookKey{}).(func()); hook != nil {
		hook()
	}
	for _, message := range msgs {
		data, err := marshalJSONContext(ctx, message)
		if err != nil {
			cleanup()
			return fmt.Errorf("encode message: %w", err)
		}
		data = append(data, '\n')
		if err := writeContextBytes(ctx, tmp, path, data); err != nil {
			cleanup()
			return fmt.Errorf("write session messages: %w", err)
		}
	}
	if err := ctx.Err(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := ctx.Err(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := fileutil.ReplaceFile(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write session messages: %w", err)
	}
	return nil
}
