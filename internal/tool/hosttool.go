package tool

import (
	"context"
	"encoding/json"
)

// HostTool is a frontend-provided capability injected through
// boot.Options.HostTools. Only local desktop controllers supply host tools
// (the built-in browser); CLI, serve, and Remote Workbench never do. The
// schema is fixed for the whole session regardless of whether the underlying
// service is available — an unavailable service fails at execution time, never
// by changing the provider-visible tool surface.
type HostTool struct {
	Name        string
	Description string
	// Schema is the JSON Schema for the tool's parameters. It is part of the
	// provider-visible prefix: it must be byte-stable across sessions.
	Schema json.RawMessage
	// ReadOnly mirrors tool.Tool.ReadOnly: it drives parallel batching and
	// plan-mode filtering.
	ReadOnly bool
	// PlanModeSafe declares the tool may run during the planning phase.
	// Read-only tools default to safe; writers must set it explicitly.
	PlanModeSafe bool
	// HostMutation marks a logically read-only tool that must first mutate
	// host state to become executable (for example starting the Browser
	// Companion process). Strict read-only agents reject these targets.
	HostMutation bool
	// Source groups the tool under an economy-mode connect_tool_source
	// source name (for example "browser"). Tools with an empty source are
	// always installed.
	Source string
	// Execute runs the tool. ExecuteWithImages, when set, is the structural
	// image channel (screenshots); the returned images never enter the text
	// result.
	Execute           func(ctx context.Context, args json.RawMessage) (string, error)
	ExecuteWithImages func(ctx context.Context, args json.RawMessage) (string, []string, error)
}

// hostToolAdapter exposes a HostTool through the tool.Tool surface while
// carrying its plan-mode and host-mutation classifiers.
type hostToolAdapter struct {
	spec HostTool
}

// NewHostTool wraps a HostTool as a tool.Tool for direct registry use (boot
// performs the same wrapping internally when installing Options.HostTools).
func NewHostTool(spec HostTool) Tool {
	return hostToolAdapter{spec: spec}
}

func (h hostToolAdapter) Name() string        { return h.spec.Name }
func (h hostToolAdapter) Description() string { return h.spec.Description }
func (h hostToolAdapter) Schema() json.RawMessage {
	return h.spec.Schema
}
func (h hostToolAdapter) ReadOnly() bool { return h.spec.ReadOnly }

func (h hostToolAdapter) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if h.spec.Execute == nil {
		return "", nil
	}
	return h.spec.Execute(ctx, args)
}

func (h hostToolAdapter) ExecuteWithImages(ctx context.Context, args json.RawMessage) (string, []string, error) {
	if h.spec.ExecuteWithImages == nil {
		text, err := h.Execute(ctx, args)
		return text, nil, err
	}
	return h.spec.ExecuteWithImages(ctx, args)
}

func (h hostToolAdapter) PlanModeSafe() bool { return h.spec.PlanModeSafe }

func (h hostToolAdapter) ReadOnlyExecutionHostMutation() bool { return h.spec.HostMutation }
