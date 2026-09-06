package tool

import (
	"context"
	"encoding/json"
)

// FileWriteIntent is local evidence about an actual prepared write. Unknown
// versions are retained as raw JSON by the session and never verified.
type FileWriteIntent struct {
	Version      int    `json:"version"`
	Path         string `json:"path"`
	Host         string `json:"host"`
	TransportID  string `json:"transport_id,omitempty"`
	Route        string `json:"route"`
	ResolvedPath string `json:"resolved_path"`
	Before       string `json:"before"`
	After        string `json:"after"`
	Encoding     string `json:"encoding"`
	Existed      bool   `json:"existed"`
}

type WriteVerification string

const (
	WriteSatisfied WriteVerification = "satisfied"
	WriteUnchanged WriteVerification = "unchanged"
	WriteConflict  WriteVerification = "conflict"
	WriteUnknown   WriteVerification = "unknown"
)

type WriteVerifier interface {
	VerifyWrite(context.Context, FileWriteIntent) WriteVerification
}
type writeIntentHookKey struct{}
type WriteIntentHook func(FileWriteIntent) error

func WithWriteIntentHook(ctx context.Context, hook WriteIntentHook) context.Context {
	return context.WithValue(ctx, writeIntentHookKey{}, hook)
}
func RecordWriteIntent(ctx context.Context, intent FileWriteIntent) error {
	if hook, ok := ctx.Value(writeIntentHookKey{}).(WriteIntentHook); ok {
		return hook(intent)
	}
	return nil
}
func DecodeWriteIntent(raw json.RawMessage) (FileWriteIntent, bool) {
	var intent FileWriteIntent
	err := json.Unmarshal(raw, &intent)
	return intent, err == nil && intent.Version == 1 && intent.Path != "" && intent.Host != "" && intent.After != ""
}

func HasWriteIntentHook(ctx context.Context) bool {
	_, ok := ctx.Value(writeIntentHookKey{}).(WriteIntentHook)
	return ok
}
