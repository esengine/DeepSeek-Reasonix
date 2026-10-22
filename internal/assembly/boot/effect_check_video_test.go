package boot

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

type checkVideoProvider struct {
	mu   sync.Mutex
	reqs []provider.Request
}

func (p *checkVideoProvider) Name() string { return "boot-check-video" }

func (p *checkVideoProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	round := len(p.reqs)
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 2)
	switch round {
	case 1:
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "s1", Name: "use_capability", Arguments: `{"action":"search","query":"technical acceptance check of a rendered video file"}`}}
	case 2:
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "c1", Name: "use_capability", Arguments: `{"action":"call","capability_id":"tool:check_video","arguments":{"path":"clip.mp4","width":640,"height":360,"duration":30,"audio":"required"}}`}}
	default:
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *checkVideoProvider) requests() []provider.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]provider.Request(nil), p.reqs...)
}

// checkVideoWorkspace stands ffmpeg and ffprobe in inside the workspace, which
// an OS sandbox always shows the programs it wraps.
func checkVideoWorkspace(t *testing.T, kind string, withPrograms bool) (home, dir string) {
	t.Helper()
	home = statedBootHome(t)
	dir = testenv.TempDir(t)
	writeUserConfigAt(t, home, "[permissions]\nallow = [\"check_video\"]\n")
	bin := filepath.Join(dir, ".stubs")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if withPrograms {
		script := func(name, body string) {
			if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		script("ffprobe", `echo '{"streams":[{"codec_type":"video","codec_name":"h264","width":640,"height":360},{"codec_type":"audio","codec_name":"aac"}],"format":{"duration":"30.0"}}'`)
		script("ffmpeg", `exit 0`)
	}
	t.Setenv("PATH", bin)
	mp4 := append([]byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, make([]byte, 64)...)
	if err := os.WriteFile(filepath.Join(dir, "clip.mp4"), mp4, 0o644); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
`)
	approveWorkspaceAt(t, home, dir)
	return home, dir
}

func TestEffectCheckVideoReportReachesTheModelThroughCapabilitySearch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in programs are shell scripts")
	}
	rec := &checkVideoProvider{}
	provider.Register("boot-check-video", func(provider.Config) (provider.Provider, error) { return rec, nil })
	home, dir := checkVideoWorkspace(t, "boot-check-video", true)
	ctrl, err := Build(context.Background(), Options{Home: home, WorkspaceRoot: dir, Sink: event.Discard, HeadlessApprovalMode: "ask"})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "is clip.mp4 technically acceptable?"); err != nil {
		t.Fatal(err)
	}
	reqs := agentRequests(rec.requests())
	if len(reqs) != 3 {
		t.Fatalf("requests = %d, want 3", len(reqs))
	}
	first, _ := json.Marshal(reqs[0].Tools)
	if strings.Contains(string(first), `"check_video"`) {
		t.Fatal("check_video joined the schema prefix; it is reached through use_capability")
	}
	for i, req := range reqs[1:] {
		current, _ := json.Marshal(req.Tools)
		if !bytes.Equal(first, current) || req.Messages[0].Content != reqs[0].Messages[0].Content {
			t.Fatalf("request %d changed the cache-stable prefix", i+1)
		}
	}
	_, searched := toolMessages(reqs[1])
	if !strings.Contains(strings.Join(searched, "\n"), "tool:check_video") {
		t.Fatal("the capability search did not offer tool:check_video")
	}
	_, told := toolMessages(reqs[2])
	report := told[len(told)-1]
	if !strings.Contains(report, `"technical":"pass"`) || !strings.Contains(report, `"unchecked":["content","motion","audio_and_sync"]`) {
		t.Fatalf("report at provider: %s", report)
	}
}

func TestEffectCheckVideoNeedsFFmpeg(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH isolation uses a POSIX layout")
	}
	rec := &checkVideoProvider{}
	provider.Register("boot-check-video-missing", func(provider.Config) (provider.Provider, error) { return rec, nil })
	home, dir := checkVideoWorkspace(t, "boot-check-video-missing", false)
	ctrl, err := Build(context.Background(), Options{Home: home, WorkspaceRoot: dir, Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	_ = ctrl.Run(context.Background(), "check clip.mp4")
	_, searched := toolMessages(agentRequests(rec.requests())[1])
	if strings.Contains(strings.Join(searched, "\n"), "tool:check_video") {
		t.Fatal("check_video offered without ffmpeg to run it")
	}
}
