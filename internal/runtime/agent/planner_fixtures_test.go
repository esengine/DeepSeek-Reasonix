package agent

import (
	"context"
	"slices"

	"reasonix/internal/contract/provider"
)

// mockProvider replays preset chunks and records the last request it received.
type mockProvider struct {
	name     string
	chunks   []provider.Chunk
	streams  [][]provider.Chunk
	lastReq  provider.Request
	requests []provider.Request
}

func lastUser(req provider.Request) string {
	for _, v := range slices.Backward(req.Messages) {
		if v.Role == provider.RoleUser {
			return v.Content
		}
	}
	return ""
}

func toolSchemaNames(schemas []provider.ToolSchema) []string {
	out := make([]string, 0, len(schemas))
	for _, s := range schemas {
		out = append(out, s.Name)
	}
	return out
}

func (m *mockProvider) Name() string { return m.name }

func (m *mockProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	m.lastReq = req
	call := len(m.requests)
	m.requests = append(m.requests, req)
	chunks := m.chunks
	if len(m.streams) > 0 {
		if call >= len(m.streams) {
			call = len(m.streams) - 1
		}
		chunks = m.streams[call]
	}
	ch := make(chan provider.Chunk, len(chunks))
	for _, c := range chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}
