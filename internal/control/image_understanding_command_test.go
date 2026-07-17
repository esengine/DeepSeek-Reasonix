package control

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/event"
)

func TestCommandImageUnderstandingWrapsJSON(t *testing.T) {
	dir := t.TempDir()
	image := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(image, mustBase64(t, tinyPNG), 0o644); err != nil {
		t.Fatal(err)
	}
	sha, err := fileSHA256(image)
	if err != nil {
		t.Fatal(err)
	}
	command := imageUnderstandingTestCommand(t, `{"visible_text":"状态栏\nYOLO","width":10,"height":20,"text_regions":[{"text":"状态栏"}],"elapsed_ms":12.3}`, "")
	iu, err := NewCommandImageUnderstanding(command)
	if err != nil {
		t.Fatal(err)
	}
	got, err := iu.DescribeImages(context.Background(), "look", []ImageUnderstandingRef{{
		Source: "@shot.png",
		Path:   image,
		SHA256: sha,
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<image-understanding source="@shot.png" sha256="` + sha + `">`,
		"visible_text:",
		"状态栏",
		"YOLO",
		"layout: 10x20; text_regions=1; ocr_ms=12.3",
		"confidence: medium",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("DescribeImages output missing %q:\n%s", want, got)
		}
	}
}

func TestCommandImageUnderstandingCachesBySHA(t *testing.T) {
	dir := t.TempDir()
	image := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(image, mustBase64(t, tinyPNG), 0o644); err != nil {
		t.Fatal(err)
	}
	sha, err := fileSHA256(image)
	if err != nil {
		t.Fatal(err)
	}
	counter := filepath.Join(dir, "runs.txt")
	command := imageUnderstandingTestCommand(t, `{"visible_text":"cached text","confidence":"high"}`, counter)
	iu, err := NewCommandImageUnderstandingForRoot(command, dir)
	if err != nil {
		t.Fatal(err)
	}
	got1, err := iu.DescribeImages(context.Background(), "look", []ImageUnderstandingRef{{
		Source: "@first.png",
		Path:   image,
		SHA256: sha,
	}})
	if err != nil {
		t.Fatal(err)
	}
	got2, err := iu.DescribeImages(context.Background(), "look again", []ImageUnderstandingRef{{
		Source: "@second.png",
		Path:   image,
		SHA256: sha,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(counter); err != nil || strings.TrimSpace(string(raw)) != "1" {
		t.Fatalf("command runs = %q, err=%v; want 1", string(raw), err)
	}
	if !strings.Contains(got1, `source="@first.png"`) || !strings.Contains(got1, "cached text") {
		t.Fatalf("first output mismatch:\n%s", got1)
	}
	if !strings.Contains(got2, `source="@second.png"`) || strings.Contains(got2, `source="@first.png"`) {
		t.Fatalf("cache hit should rewrite source for current ref:\n%s", got2)
	}
	if _, err := os.Stat(ImageUnderstandingCachePathForRoot(dir)); err != nil {
		t.Fatalf("cache was not written: %v", err)
	}
}

func TestControllerWithImageUnderstandingInjectsOnlyForTextOnlyModel(t *testing.T) {
	workspace := t.TempDir()
	cfg := config.Default()
	cfg.DefaultModel = "custom/text-only"
	cfg.Providers = []config.ProviderEntry{{
		Name:         "custom",
		Kind:         "openai",
		BaseURL:      "https://example.invalid/v1",
		Models:       []string{"text-only", "vision-pro"},
		VisionModels: []string{"vision-pro"},
	}}
	if err := cfg.SaveTo(filepath.Join(workspace, "reasonix.toml")); err != nil {
		t.Fatalf("save workspace config: %v", err)
	}
	image := filepath.Join(workspace, "shot.png")
	if err := os.WriteFile(image, mustBase64(t, tinyPNG), 0o644); err != nil {
		t.Fatal(err)
	}
	command := imageUnderstandingTestCommand(t, `{"visible_text":"button text","confidence":"high"}`, "")
	iu, err := NewCommandImageUnderstanding(command)
	if err != nil {
		t.Fatal(err)
	}
	c := New(Options{WorkspaceRoot: workspace, ImageUnderstanding: iu, ModelRef: "custom/text-only"})
	got := c.withImageUnderstanding(context.Background(), "see @shot.png")
	if !strings.HasPrefix(got, "Image understanding context:\n\n<image-understanding ") {
		t.Fatalf("withImageUnderstanding did not prepend image context:\n%s", got)
	}
	if !strings.Contains(got, `source="@shot.png"`) || !strings.Contains(got, "button text") || !strings.HasSuffix(got, "\n\nsee @shot.png") {
		t.Fatalf("withImageUnderstanding output mismatch:\n%s", got)
	}

	c.modelRef = "custom/vision-pro"
	if got := c.withImageUnderstanding(context.Background(), "see @shot.png"); got != "see @shot.png" {
		t.Fatalf("vision model should receive direct image input without sidecar, got:\n%s", got)
	}
}

func TestControllerImageUnderstandingNoticeCarriesDisclosureDetail(t *testing.T) {
	workspace := t.TempDir()
	cfg := config.Default()
	cfg.DefaultModel = "custom/text-only"
	cfg.Providers = []config.ProviderEntry{{
		Name:    "custom",
		Kind:    "openai",
		BaseURL: "https://example.invalid/v1",
		Models:  []string{"text-only"},
	}}
	if err := cfg.SaveTo(filepath.Join(workspace, "reasonix.toml")); err != nil {
		t.Fatalf("save workspace config: %v", err)
	}
	image := filepath.Join(workspace, "shot.png")
	if err := os.WriteFile(image, mustBase64(t, tinyPNG), 0o644); err != nil {
		t.Fatal(err)
	}
	command := imageUnderstandingTestCommand(t, `{"visible_text":"statusbar text","confidence":"high"}`, "")
	iu, err := NewCommandImageUnderstanding(command)
	if err != nil {
		t.Fatal(err)
	}
	sink := &noticeSink{}
	c := New(Options{Sink: sink, WorkspaceRoot: workspace, ImageUnderstanding: iu, ImageUnderstandingLog: "summary", ModelRef: "custom/text-only"})
	_ = c.withImageUnderstanding(context.Background(), "see @shot.png")

	sink.mu.Lock()
	defer sink.mu.Unlock()
	var got *event.Event
	for i := range sink.events {
		if sink.events[i].Kind == event.Notice && strings.HasPrefix(sink.events[i].Text, "image understood:") {
			got = &sink.events[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("missing image understood notice: %+v", sink.events)
	}
	if got.Source != event.UsageSourceVision {
		t.Fatalf("notice source = %q, want vision", got.Source)
	}
	if !strings.Contains(got.Text, "1 image") || !strings.Contains(got.Text, "OCR + UI state") {
		t.Fatalf("notice text missing summary fields: %q", got.Text)
	}
	if !strings.Contains(got.Detail, "<image-understanding ") || !strings.Contains(got.Detail, "statusbar text") {
		t.Fatalf("notice detail missing disclosure body:\n%s", got.Detail)
	}
}

type imageUnderstandingRunRecorder struct {
	input string
}

func (r *imageUnderstandingRunRecorder) Run(_ context.Context, input string) error {
	r.input = input
	return nil
}

func TestRunPrependsImageUnderstandingAfterCompose(t *testing.T) {
	workspace := t.TempDir()
	cfg := config.Default()
	cfg.DefaultModel = "custom/text-only"
	cfg.Providers = []config.ProviderEntry{{
		Name:    "custom",
		Kind:    "openai",
		BaseURL: "https://example.invalid/v1",
		Models:  []string{"text-only"},
	}}
	if err := cfg.SaveTo(filepath.Join(workspace, "reasonix.toml")); err != nil {
		t.Fatalf("save workspace config: %v", err)
	}
	image := filepath.Join(workspace, "shot.png")
	if err := os.WriteFile(image, mustBase64(t, tinyPNG), 0o644); err != nil {
		t.Fatal(err)
	}
	command := imageUnderstandingTestCommand(t, `{"visible_text":"compose path text","confidence":"high"}`, "")
	iu, err := NewCommandImageUnderstanding(command)
	if err != nil {
		t.Fatal(err)
	}
	runner := &imageUnderstandingRunRecorder{}
	c := New(Options{Runner: runner, WorkspaceRoot: workspace, ImageUnderstanding: iu, ModelRef: "custom/text-only"})
	if err := c.Run(context.Background(), "see @shot.png"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Image understanding context:",
		"compose path text",
		"see @shot.png",
	} {
		if !strings.Contains(runner.input, want) {
			t.Fatalf("Run input missing %q:\n%s", want, runner.input)
		}
	}
}

func imageUnderstandingTestCommand(t *testing.T, payload, counterPath string) string {
	t.Helper()
	args := []string{os.Args[0], "-test.run=TestImageUnderstandingCommandHelper", "--", payload}
	if counterPath != "" {
		args = append(args, counterPath)
	}
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, shellQuoteTestArg(arg))
	}
	return strings.Join(quoted, " ")
}

func shellQuoteTestArg(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func TestImageUnderstandingCommandHelper(t *testing.T) {
	marker := -1
	for i, arg := range os.Args {
		if arg == "--" {
			marker = i
			break
		}
	}
	if marker < 0 || marker+1 >= len(os.Args) {
		return
	}
	payload := os.Args[marker+1]
	if marker+2 < len(os.Args) {
		counterPath := os.Args[marker+2]
		n := 0
		if raw, err := os.ReadFile(counterPath); err == nil {
			n, _ = strconv.Atoi(strings.TrimSpace(string(raw)))
		}
		if err := os.WriteFile(counterPath, []byte(strconv.Itoa(n+1)), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}
	fmt.Fprintln(os.Stdout, payload)
	os.Exit(0)
}
