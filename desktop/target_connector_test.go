package main

import (
	"context"
	"errors"
	"testing"
)

type reconnectingTestConnector struct {
	connectCalls    int
	reconnectCalls  int
	connectResult   TargetAdapter
	reconnectResult TargetAdapter
	err             error
}

func (c *reconnectingTestConnector) Connect(context.Context, TargetDescriptor) (TargetAdapter, error) {
	c.connectCalls++
	return c.connectResult, c.err
}

func (c *reconnectingTestConnector) Reconnect(context.Context, TargetDescriptor, TargetAdapter) (TargetAdapter, error) {
	c.reconnectCalls++
	return c.reconnectResult, c.err
}

func TestTargetConnectorMuxRoutesWithoutFallback(t *testing.T) {
	local := &reconnectingTestConnector{}
	want := errors.New("remote authentication failed")
	remote := &reconnectingTestConnector{err: want}
	mux := TargetConnectorMux{Local: local, Remote: remote}

	_, err := mux.Connect(context.Background(), TargetDescriptor{Kind: TargetRemote, ID: "host-1"})
	if !errors.Is(err, want) {
		t.Fatalf("Remote connect error = %v, want %v", err, want)
	}
	if remote.connectCalls != 1 || local.connectCalls != 0 {
		t.Fatalf("connector calls local=%d remote=%d", local.connectCalls, remote.connectCalls)
	}
}

func TestTargetConnectorMuxReconnectUsesRemoteRecoveryOnly(t *testing.T) {
	remote := &reconnectingTestConnector{}
	mux := TargetConnectorMux{Remote: remote}
	target := TargetDescriptor{Kind: TargetRemote, ID: "host-1"}

	_, _ = mux.Reconnect(context.Background(), target, nil)
	if remote.reconnectCalls != 1 || remote.connectCalls != 0 {
		t.Fatalf("remote reconnect/connect calls = %d/%d", remote.reconnectCalls, remote.connectCalls)
	}
	if _, err := mux.Reconnect(context.Background(), TargetDescriptor{Kind: TargetLocal, ID: "local"}, nil); !errors.Is(err, ErrTargetReconnectUnsupported) {
		t.Fatalf("Local reconnect error = %v", err)
	}
}
