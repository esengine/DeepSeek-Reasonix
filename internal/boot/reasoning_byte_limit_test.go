package boot

// Reasoning-byte-limit assembly test: config.reasoning_byte_limit must reach
// the executor through the real Build stack and bound stored hidden reasoning.

import (
	"context"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

type reasoningLimitProvider struct{}

func (p *reasoningLimitProvider) Name() string { return "boot-reasoning-limit" }

func (p *reasoningLimitProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 3)
	ch <- provider.Chunk{Type: provider.ChunkReasoning, Text: strings.Repeat("r", 64)}
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestEffectReasoningByteLimitReachesExecutor(t *testing.T) {
	provider.Register("boot-reasoning-limit", func(provider.Config) (provider.Provider, error) {
		return &reasoningLimitProvider{}, nil
	})
	tests := []struct {
		name    string
		setting string
		wantLen int
	}{
		{name: "custom cap bounds stored reasoning", setting: "reasoning_byte_limit = 16", wantLen: 16},
		{name: "negative disables the limit", setting: "reasoning_byte_limit = -1", wantLen: 64},
		{name: "unset keeps the built-in limit", setting: "", wantLen: 64},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateConfigHome(t)
			dir := robustTempDir(t)
			t.Chdir(dir)

			writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"
`+tt.setting+`

[[providers]]
name = "test-model"
kind = "boot-reasoning-limit"
model = "x"
`)

			ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			defer ctrl.Close()
			if err := ctrl.Run(context.Background(), "reply ok"); err != nil {
				t.Fatalf("Run: %v", err)
			}

			msgs := ctrl.Executor().Session().Messages
			var rc string
			for _, m := range slices.Backward(msgs) {
				if m.Role == provider.RoleAssistant && m.ReasoningContent != "" {
					rc = m.ReasoningContent
					break
				}
			}
			if rc == "" {
				t.Fatal("no stored reasoning found in the executor session")
			}
			if len(rc) != tt.wantLen {
				t.Fatalf("stored reasoning = %d bytes, want %d (reasoning_byte_limit not assembled)", len(rc), tt.wantLen)
			}
		})
	}
}
