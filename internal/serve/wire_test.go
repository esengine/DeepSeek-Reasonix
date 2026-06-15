package serve

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestToWire(t *testing.T) {
	t.Run("tool dispatch", func(t *testing.T) {
		w := toWire(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{Name: "bash", Args: `{"cmd":"ls"}`, ReadOnly: false}})
		if w.Kind != "tool_dispatch" || w.Tool == nil || w.Tool.Name != "bash" || w.Tool.Args != `{"cmd":"ls"}` {
			t.Errorf("dispatch = %+v / %+v", w, w.Tool)
		}
	})

	t.Run("tool dispatch profile", func(t *testing.T) {
		w := toWire(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{
			Name: "task", Args: `{"prompt":"x"}`,
			Profile: &event.Profile{Model: "deepseek-pro", Effort: "max"},
		}})
		if w.Tool == nil || w.Tool.Profile == nil || w.Tool.Profile.Model != "deepseek-pro" || w.Tool.Profile.Effort != "max" {
			t.Errorf("profile = %+v", w.Tool)
		}
	})

	t.Run("tool dispatch file diff", func(t *testing.T) {
		w := toWire(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{
			Name:     "edit_file",
			Args:     `{"path":"settings/settings_IO.gd"}`,
			FileDiff: event.FileDiff{Diff: "@@ -27 +27 @@\n-old\n+new\n", Added: 1, Removed: 1},
		}})
		if w.Tool == nil {
			t.Fatal("missing tool")
		}
		b, err := json.Marshal(w.Tool)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		s := string(b)
		if !strings.Contains(s, `"diff":"@@ -27 +27 @@\n-old\n+new\n"`) || !strings.Contains(s, `"added":1`) || !strings.Contains(s, `"removed":1`) {
			t.Fatalf("tool file diff was not serialized: %s", s)
		}
	})

	t.Run("tool result duration", func(t *testing.T) {
		w := toWire(event.Event{Kind: event.ToolResult, Tool: event.Tool{Name: "web_fetch", Output: "ok", DurationMs: 522}})
		if w.Tool == nil || w.Tool.Output != "ok" || w.Tool.DurationMs != 522 {
			t.Errorf("tool result duration = %+v", w.Tool)
		}
	})

	t.Run("usage with cost", func(t *testing.T) {
		w := toWire(event.Event{
			Kind:        event.Usage,
			Usage:       &provider.Usage{PromptTokens: 1000, CompletionTokens: 200, TotalTokens: 1200, CacheHitTokens: 900, CacheMissTokens: 100},
			Pricing:     &provider.Pricing{CacheHit: 0.02, Input: 1, Output: 2},
			UsageSource: event.UsageSourceTitle,
			CacheDiagnostics: &event.CacheDiagnostics{
				PrefixChanged:       true,
				PrefixChangeReasons: []string{"log_rewrite"},
				LogRewriteVersion:   1,
			},
		})
		if w.Usage == nil || w.Usage.TotalTokens != 1200 || w.Usage.Cost <= 0 || w.Usage.CostUSD <= 0 || w.Usage.Currency != "¥" {
			t.Errorf("usage = %+v", w.Usage)
		}
		if w.Usage.Source != event.UsageSourceTitle {
			t.Errorf("usage source = %q, want title", w.Usage.Source)
		}
		if w.Usage.CacheDiagnostics == nil || w.Usage.CacheDiagnostics.PrefixChangeReasons[0] != "log_rewrite" {
			t.Errorf("cache diagnostics = %+v", w.Usage.CacheDiagnostics)
		}
	})

	t.Run("notice warn", func(t *testing.T) {
		w := toWire(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "truncated"})
		if w.Kind != "notice" || w.Level != "warn" || w.Text != "truncated" {
			t.Errorf("notice = %+v", w)
		}
	})

	t.Run("approval", func(t *testing.T) {
		w := toWire(event.Event{Kind: event.ApprovalRequest, Approval: event.Approval{ID: "3", Tool: "bash", Subject: "rm"}})
		if w.Approval == nil || w.Approval.ID != "3" || w.Approval.Tool != "bash" {
			t.Errorf("approval = %+v", w.Approval)
		}
	})

	t.Run("turn done error", func(t *testing.T) {
		w := toWire(event.Event{Kind: event.TurnDone, Err: errors.New("boom")})
		if w.Kind != "turn_done" || w.Err != "boom" {
			t.Errorf("turn_done = %+v", w)
		}
	})

	t.Run("steer", func(t *testing.T) {
		w := toWire(event.Event{Kind: event.Steer, Text: "mid-turn guidance"})
		if w.Kind != "steer" || w.Text != "mid-turn guidance" {
			t.Errorf("steer = %+v", w)
		}
	})
}
