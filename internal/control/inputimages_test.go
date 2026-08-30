package control

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/provider"
)

func writeVisionTestConfig(t *testing.T, root string) {
	t.Helper()
	cfg := config.Default()
	cfg.DefaultModel = "custom/vision-pro"
	cfg.Providers = []config.ProviderEntry{{
		Name:         "custom",
		Kind:         "openai",
		BaseURL:      "https://example.invalid/v1",
		Models:       []string{"text-only", "vision-pro"},
		VisionModels: []string{"vision-pro"},
	}}
	if err := cfg.SaveTo(filepath.Join(root, "reasonix.toml")); err != nil {
		t.Fatalf("save config: %v", err)
	}
}

func TestControllerInputImagesResolvesAttachment(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeVisionTestConfig(t, dir)
	ref, err := SaveImageDataURL("data:image/png;base64," + tinyPNG)
	if err != nil {
		t.Fatalf("SaveImageDataURL: %v", err)
	}
	urls := (&Controller{modelRef: "custom/vision-pro"}).inputImages("look at @" + ref)
	if len(urls) != 1 {
		t.Fatalf("inputImages = %v, want one resolved data URL", urls)
	}
	if !strings.HasPrefix(urls[0], "data:image/png;base64,") {
		t.Errorf("resolved url = %q, want a png data URL", urls[0])
	}
}

func TestControllerInputImagesIgnoresNonAttachmentRefs(t *testing.T) {
	t.Chdir(t.TempDir())
	if urls := New(Options{}).inputImages("plain text with @missing.png"); len(urls) != 0 {
		t.Errorf("inputImages = %v, want none for a non-existent / non-attachment ref", urls)
	}
}

func TestControllerInputImagesResolvesWorkspaceImage(t *testing.T) {
	workspace := t.TempDir()
	writeVisionTestConfig(t, workspace)
	path := filepath.Join(workspace, "docs", "diagram.png")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, mustBase64(t, tinyPNG), 0o644); err != nil {
		t.Fatal(err)
	}

	urls := (&Controller{workspaceRoot: workspace, modelRef: "custom/vision-pro"}).inputImages("look at @docs/diagram.png")
	if len(urls) != 1 {
		t.Fatalf("inputImages = %v, want one resolved data URL", urls)
	}
	if !strings.HasPrefix(urls[0], "data:image/png;base64,") {
		t.Errorf("resolved url = %q, want a png data URL", urls[0])
	}
}

func TestControllerInputImagesResolvesAbsoluteWorkspaceImage(t *testing.T) {
	workspace := t.TempDir()
	writeVisionTestConfig(t, workspace)
	path := filepath.Join(workspace, "diagram.png")
	if err := os.WriteFile(path, mustBase64(t, tinyPNG), 0o644); err != nil {
		t.Fatal(err)
	}

	urls := (&Controller{workspaceRoot: workspace, modelRef: "custom/vision-pro"}).inputImages("look at @" + path)
	if len(urls) != 1 {
		t.Fatalf("inputImages = %v, want one resolved data URL", urls)
	}
	if !strings.HasPrefix(urls[0], "data:image/png;base64,") {
		t.Errorf("resolved url = %q, want a png data URL", urls[0])
	}
}

func TestControllerInputImagesRequiresWorkspaceForFileImageRefs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "diagram.png")
	if err := os.WriteFile(path, mustBase64(t, tinyPNG), 0o644); err != nil {
		t.Fatal(err)
	}

	urls := New(Options{}).inputImages("look at @" + path)
	if len(urls) != 0 {
		t.Fatalf("inputImages without a workspace = %v, want no file image refs", urls)
	}
}

func TestControllerInputImagesSkipsModelImagesWhenSelectedModelIsTextOnly(t *testing.T) {
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
	path := filepath.Join(workspace, "diagram.png")
	if err := os.WriteFile(path, mustBase64(t, tinyPNG), 0o644); err != nil {
		t.Fatal(err)
	}

	c := &Controller{workspaceRoot: workspace, modelRef: "custom/text-only"}
	if urls := c.inputImages("look at @diagram.png"); len(urls) != 0 {
		t.Fatalf("text-only model should suppress image payloads, got %v", urls)
	}

	c.modelRef = "custom/vision-pro"
	if urls := c.inputImages("look at @diagram.png"); len(urls) != 1 {
		t.Fatalf("vision model should keep image payloads, got %v", urls)
	}
}

func TestControllerResolvesSubagentImageCandidatesForTextParent(t *testing.T) {
	workspace := t.TempDir()
	cfg := config.Default()
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
	path := filepath.Join(workspace, "diagram.png")
	if err := os.WriteFile(path, mustBase64(t, tinyPNG), 0o644); err != nil {
		t.Fatal(err)
	}

	c := &Controller{workspaceRoot: workspace, modelRef: "custom/text-only"}
	if urls := c.inputImages("look at @diagram.png"); len(urls) != 0 {
		t.Fatalf("text-only parent should suppress its own image payload, got %v", urls)
	}
	if urls := c.resolveInputImageCandidates("look at @diagram.png"); len(urls) != 1 {
		t.Fatalf("subagent image candidates = %v, want one image for a vision child", urls)
	}
}

func TestControllerResolveTurnImagesReusesCandidatesForVisionParent(t *testing.T) {
	workspace := t.TempDir()
	writeVisionTestConfig(t, workspace)
	path := filepath.Join(workspace, "diagram.png")
	if err := os.WriteFile(path, mustBase64(t, tinyPNG), 0o644); err != nil {
		t.Fatal(err)
	}

	c := &Controller{workspaceRoot: workspace, modelRef: "custom/vision-pro"}
	userImages, candidates := c.resolveTurnImages("inspect @diagram.png")
	if len(userImages) != 1 || len(candidates) != 1 {
		t.Fatalf("turn images = %v, candidates = %v; want one image in both paths", userImages, candidates)
	}
	if &userImages[0] != &candidates[0] || userImages[0] != candidates[0] {
		t.Fatal("vision parent and subagent candidates should reuse the same resolved image slice")
	}

	c.modelRef = "custom/text-only"
	userImages, candidates = c.resolveTurnImages("inspect @diagram.png")
	if len(userImages) != 0 || len(candidates) != 1 {
		t.Fatalf("text parent turn images = %v, candidates = %v; want candidates only", userImages, candidates)
	}
}

func TestGoalContinuationKeepsCurrentTurnImageCandidatesWithoutCrossTurnLeak(t *testing.T) {
	workspace := t.TempDir()
	writeVisionTestConfig(t, workspace)
	path := filepath.Join(workspace, "diagram.png")
	if err := os.WriteFile(path, mustBase64(t, tinyPNG), 0o644); err != nil {
		t.Fatal(err)
	}

	c := &Controller{workspaceRoot: workspace, modelRef: "custom/text-only"}
	initial := c.prepareOrchestratedTurnImages(orchestratedTurn{
		raw:       "inspect the diagnostic",
		imageRefs: "@diagram.png",
	})
	if len(initial.userImages) != 0 || len(initial.imageCandidates) != 1 {
		t.Fatalf("initial turn images = %v, candidates = %v; want child-only candidate", initial.userImages, initial.imageCandidates)
	}

	ctx := agent.WithSubagentImageCandidates(context.Background(), initial.imageCandidates)
	continuation := orchestratedTurn{goalContinuation: &goalContinuationSnapshot{}, synthetic: true, raw: goalContinueTurn}
	userImages, candidates := c.imagesForOrchestratedTurn(ctx, continuation)
	if len(userImages) != 0 || len(candidates) != 1 || candidates[0] != initial.imageCandidates[0] {
		t.Fatalf("Goal continuation images = %v, candidates = %v; want original child candidate only", userImages, candidates)
	}

	next := c.prepareOrchestratedTurnImages(orchestratedTurn{raw: "plain next user turn"})
	ctx = agent.WithSubagentImageCandidates(ctx, next.imageCandidates)
	userImages, candidates = c.imagesForOrchestratedTurn(ctx, continuation)
	if len(userImages) != 0 || len(candidates) != 0 {
		t.Fatalf("next user turn leaked prior image: images = %v, candidates = %v", userImages, candidates)
	}
}

func TestControllerImageInputEnabledDoesNotFallbackFromUnknownRef(t *testing.T) {
	workspace := t.TempDir()
	writeVisionTestConfig(t, workspace)

	c := &Controller{workspaceRoot: workspace, modelRef: "deleted/model"}
	if c.imageInputEnabled() {
		t.Fatal("unknown ref should not inherit image input from the default fallback model")
	}
}

func TestResolveRefsVisionCapableImageDoesNotAskForOCR(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeVisionTestConfig(t, dir)
	const slashPath = ".reasonix/attachments/shot.png"
	if err := os.MkdirAll(filepath.Dir(slashPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(slashPath, []byte("\x89PNG\r\n\x1a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := &Controller{workspaceRoot: dir, modelRef: "custom/vision-pro"}
	block, errs := c.ResolveRefs(context.Background(), "这是什么？ @"+slashPath)
	if len(errs) != 0 {
		t.Fatalf("ResolveRefs errors = %v", errs)
	}
	if !strings.Contains(block, `<image path="`+slashPath+`">`) || !strings.Contains(block, "attached as visual input") {
		t.Fatalf("vision-capable attachment should mark visual input:\n%s", block)
	}
	if strings.Contains(block, "OCR/image/vision tool") || strings.Contains(block, "image bytes are not inlined") {
		t.Fatalf("vision-capable attachment must not tell the model to OCR the file:\n%s", block)
	}
	if urls := c.inputImages("这是什么？ @" + slashPath); len(urls) != 1 || !strings.HasPrefix(urls[0], "data:image/png;base64,") {
		t.Fatalf("vision-capable inputImages = %v, want one png data URL", urls)
	}
}

func TestControllerInputImagesPassesHTTPURLAndFileID(t *testing.T) {
	workspace := t.TempDir()
	writeVisionTestConfig(t, workspace)
	c := &Controller{workspaceRoot: workspace, modelRef: "custom/vision-pro"}
	urls := c.inputImages("see @https://cdn.example.com/cat.png and @file-api-0a1b2c3d4e5f6071")
	if len(urls) != 2 || urls[0] != "https://cdn.example.com/cat.png" || urls[1] != "file-api-0a1b2c3d4e5f6071" {
		t.Fatalf("inputImages = %v, want URL then file_id", urls)
	}
	bare := c.inputImages("这是什么？ https://cdn.example.com/dog.webp")
	if len(bare) != 1 || bare[0] != "https://cdn.example.com/dog.webp" {
		t.Fatalf("bare URL inputImages = %v", bare)
	}
}

func TestControllerUploadsLargeOfficialDeepSeekImageViaFilesAPI(t *testing.T) {
	workspace := t.TempDir()
	t.Chdir(workspace)
	cfg := config.Default()
	cfg.DefaultModel = "deepseek/deepseek-v4-flash-vision-exp"
	cfg.Providers = []config.ProviderEntry{{
		Name:         "deepseek",
		Kind:         "openai",
		BaseURL:      "https://api.deepseek.com",
		Models:       []string{"deepseek-v4-flash-vision-exp"},
		VisionModels: []string{"deepseek-v4-flash-vision-exp"},
		APIKeyEnv:    "DEEPSEEK_API_KEY",
	}}
	if err := cfg.SaveTo(filepath.Join(workspace, "reasonix.toml")); err != nil {
		t.Fatal(err)
	}
	prevLimit := inlineImageLimit
	inlineImageLimit = 4
	t.Cleanup(func() { inlineImageLimit = prevLimit })
	prevUpload := uploadVisionFile
	uploadVisionFile = func(_ context.Context, u provider.FileUpload) (string, error) {
		if u.Protocol != "openai" || len(u.Data) <= 4 {
			t.Fatalf("upload = %+v", u)
		}
		return "file-api-uploaded0001", nil
	}
	t.Cleanup(func() { uploadVisionFile = prevUpload })

	path := filepath.Join(workspace, ".reasonix", "attachments", "big.png")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte("x"), 8)...)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Controller{workspaceRoot: workspace, modelRef: "deepseek/deepseek-v4-flash-vision-exp"}
	got := c.inputImages("look at @.reasonix/attachments/big.png")
	if len(got) != 1 || got[0] != "file-api-uploaded0001" {
		t.Fatalf("inputImages = %v, want uploaded file_id", got)
	}
}
