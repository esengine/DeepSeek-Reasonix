package sessionstore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/store"
)

func writeNativeAppendLog(t *testing.T, bodyBytes, count int) (path string, log []byte) {
	t.Helper()
	path = filepath.Join(testenv.TempDir(t), "20261027-100000.000000000-deepseek-flash.jsonl")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	emit := func(rec map[string]any) {
		rec["schema_version"] = 1
		line, err := json.Marshal(rec)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	emit(map[string]any{"type": "replace", "revision": 1, "messages": []any{map[string]any{"role": "system", "content": "system prompt"}}})
	body := strings.Repeat("x", bodyBytes)
	for i := range count {
		emit(map[string]any{"type": "append", "revision": i + 2, "base_revision": i + 1, "message_index": 1 + i, "messages": []any{map[string]any{"role": "user", "content": fmt.Sprintf("%03d%s", i, body)}}})
	}
	log = []byte(b.String())
	if err := os.WriteFile(store.SessionEventLog(path), log, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, log
}

func TestAHistoryOverThePayloadBoundStillHasATail(t *testing.T) {
	path, log := writeNativeAppendLog(t, 100, 12)
	limits := defaultSessionReplayLimits
	limits.maxPayloadBytes = 600

	if _, _, _, err := loadSessionMessagesWithLimits(path, limits); err == nil {
		t.Fatal("the full history loaded under a bound it exceeds")
	}
	tail, err := loadSessionTailWithLimits(path, limits, 600)
	if err != nil {
		t.Fatal(err)
	}
	if len(tail.Messages) < 3 || tail.Dropped == 0 {
		t.Fatalf("tail = %d messages, %d dropped; want a bounded recent part and the count left out", len(tail.Messages), tail.Dropped)
	}
	if tail.Messages[0].Role != provider.RoleSystem {
		t.Fatalf("first message = %q, want the system prompt kept", tail.Messages[0].Role)
	}
	last := tail.Messages[len(tail.Messages)-1].Content
	if !strings.HasPrefix(last, "011") {
		t.Fatalf("last message = %q, want the newest", last)
	}
	prev := tail.Messages[len(tail.Messages)-2].Content
	if !strings.HasPrefix(prev, "010") {
		t.Fatalf("the part kept is not contiguous: %q before %q", prev, last)
	}
	if len(tail.Messages)-1+tail.Dropped != 12 {
		t.Fatalf("kept %d + dropped %d does not account for the 12 messages", len(tail.Messages)-1, tail.Dropped)
	}
	if got, err := os.ReadFile(store.SessionEventLog(path)); err != nil || string(got) != string(log) {
		t.Fatalf("the log changed while its tail was read: err=%v", err)
	}
}

func TestAHistoryThatFitsHasNothingLeftOut(t *testing.T) {
	path, _ := writeNativeAppendLog(t, 50, 4)
	tail, err := loadSessionTailWithLimits(path, defaultSessionReplayLimits, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if tail.Dropped != 0 || len(tail.Messages) != 5 {
		t.Fatalf("tail = %d messages, %d dropped, want all 5 and none dropped", len(tail.Messages), tail.Dropped)
	}
}

func TestADAGHistoryOverTheBoundHasATailToo(t *testing.T) {
	path := writeReplayMemoryFixture(t, 65, 1<<20, false)
	if _, _, _, err := loadSessionMessagesWithLimits(path, defaultSessionReplayLimits); err == nil {
		t.Fatal("the full history loaded under a bound it exceeds")
	}
	tail, err := loadSessionTailWithLimits(path, defaultSessionReplayLimits, 8<<20)
	if err != nil {
		t.Fatal(err)
	}
	if tail.Dropped < 50 || len(tail.Messages) > 12 {
		t.Fatalf("tail = %d messages, %d dropped, want a recent part of about 8 MiB", len(tail.Messages), tail.Dropped)
	}
	if got := tail.Messages[len(tail.Messages)-1].Content; got != "done" {
		t.Fatalf("last message = %q, want the newest", got)
	}
}

func writeNativeLog(t *testing.T, recs []map[string]any) string {
	t.Helper()
	path := filepath.Join(testenv.TempDir(t), "20261027-100000.000000000-deepseek-flash.jsonl")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, rec := range recs {
		rec["schema_version"] = 1
		line, err := json.Marshal(rec)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(store.SessionEventLog(path), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func msg(role, content string) map[string]any {
	return map[string]any{"role": role, "content": content}
}

func TestATailAfterAReplaceRecordCountsOnlyWhatFollowsIt(t *testing.T) {
	body := strings.Repeat("x", 100)
	var folded []any
	folded = append(folded, msg("system", "s"))
	for i := range 6 {
		folded = append(folded, msg("user", fmt.Sprintf("u%d %s", i, body)), msg("assistant", fmt.Sprintf("a%d %s", i, body)))
	}
	path := writeNativeLog(t, []map[string]any{
		{"type": "replace", "revision": 1, "messages": []any{msg("system", strings.Repeat("y", 3000)), msg("user", "old")}},
		{"type": "replace", "revision": 2, "messages": folded},
		{"type": "append", "revision": 3, "base_revision": 2, "message_index": len(folded), "messages": []any{msg("user", "newest")}},
	})
	limits := defaultSessionReplayLimits
	tail, err := loadSessionTailWithLimits(path, limits, 600)
	if err != nil {
		t.Fatal(err)
	}
	if got := tail.Messages[len(tail.Messages)-1].Content; got != "newest" {
		t.Fatalf("last message = %q, want the newest", got)
	}
	var held int
	for _, m := range tail.Messages[1:] {
		raw, _ := json.Marshal(m)
		held += len(raw)
	}
	if held > 600 || held < 450 {
		t.Fatalf("the tail holds %d bytes against a budget of 600: a size recorded for the replaced history was counted again", held)
	}
	if len(tail.Messages)-1+tail.Dropped != len(folded) {
		t.Fatalf("kept %d + dropped %d does not account for the %d messages after the system prompt", len(tail.Messages)-1, tail.Dropped, len(folded))
	}
}

func TestATailNeverOpensOnAToolResult(t *testing.T) {
	recs := []map[string]any{
		{"type": "replace", "revision": 1, "messages": []any{msg("system", "s"), msg("user", "one long job")}},
	}
	index := 2
	for i := range 8 {
		recs = append(recs, map[string]any{"type": "append", "revision": i + 2, "base_revision": i + 1, "message_index": index,
			"messages": []any{msg("assistant", strings.Repeat("c", 300)), msg("tool", strings.Repeat("r", 50))}})
		index += 2
	}
	path := writeNativeLog(t, recs)
	for budget := int64(300); budget < 2000; budget += 25 {
		tail, err := loadSessionTailWithLimits(path, defaultSessionReplayLimits, budget)
		if err != nil {
			t.Fatal(err)
		}
		if first := tail.Messages[1]; first.Role == "tool" {
			t.Fatalf("budget %d: the tail opens on a tool result with no call before it", budget)
		}
	}
}
