package tool

import (
	"context"
	"encoding/json"
	"io"
)

// RemoteTask identifies accepted work; it never contains a replayable submission.
type RemoteTask struct {
	Server          string `json:"server"`
	Tool            string `json:"tool"`
	TaskID          string `json:"taskId"`
	Binding         string `json:"binding"`
	Protocol        string `json:"protocol"`
	Deadline        int64  `json:"deadline"`
	CancelRequested bool   `json:"-"`
}

type RemoteTaskResult struct {
	Result   json.RawMessage
	Output   string
	Terminal bool
}

type RemoteTaskRun func(context.Context, io.Writer) (RemoteTaskResult, error)
type RemoteTaskResumer func(context.Context, RemoteTask, io.Writer) (RemoteTaskResult, error)

type RemoteTaskHandle struct {
	ID      string
	Wait    func(context.Context) (json.RawMessage, error)
	Cancel  func()
	Promote func()
}

type RemoteTaskStarter func(RemoteTask, RemoteTaskRun) (RemoteTaskHandle, error)
type remoteTaskStarterKey struct{}
type remoteTaskDetachedKey struct{}

func WithRemoteTaskStarter(ctx context.Context, start RemoteTaskStarter) context.Context {
	return context.WithValue(ctx, remoteTaskStarterKey{}, start)
}

func RemoteTaskStarterFrom(ctx context.Context) (RemoteTaskStarter, bool) {
	start, _ := ctx.Value(remoteTaskStarterKey{}).(RemoteTaskStarter)
	return start, start != nil
}

func WithRemoteTaskDetach(ctx context.Context, detached func() bool) context.Context {
	return context.WithValue(ctx, remoteTaskDetachedKey{}, detached)
}

func RemoteTaskDetached(ctx context.Context) bool {
	detached, _ := ctx.Value(remoteTaskDetachedKey{}).(func() bool)
	return detached != nil && detached()
}
