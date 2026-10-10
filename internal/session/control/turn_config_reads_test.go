package control

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/runtime/agent"
)

func countConfigReads(t *testing.T) *int {
	t.Helper()
	reads := 0
	prev := loadConfigForRoot
	loadConfigForRoot = func(root string) (*config.Config, error) {
		reads++
		return prev(root)
	}
	t.Cleanup(func() { loadConfigForRoot = prev })
	return &reads
}

func TestATurnWithoutAttachmentsReadsNoConfigForImages(t *testing.T) {
	workspace := testenv.TempDir(t)
	writeVisionTestConfig(t, workspace)
	c := &Controller{controllerDeps: controllerDeps{workspaceRoot: workspace, modelRef: "custom/vision-pro"}}
	reads := countConfigReads(t)

	images := c.resolveTurnImages("just words, no attachment")
	ctx, _ := c.withTurnImages(context.Background(), "just words, no attachment")
	c.withVisionRouting(agent.WithSubagentImageCandidates(ctx, nil))
	c.imagesForOrchestratedTurn(context.Background(), orchestratedTurn{goalContinuation: &goalContinuationSnapshot{}})

	if len(images.candidates) != 0 || len(images.userImages) != 0 {
		t.Fatalf("a turn with no attachment carries images: %v / %v", images.userImages, images.candidates)
	}
	if *reads != 0 {
		t.Fatalf("a turn with no attachment read the config %d time(s) for image routing", *reads)
	}
}

func TestAnAttachmentStillResolvesTheModelAndTheVisionReader(t *testing.T) {
	workspace := testenv.TempDir(t)
	writeVisionTestConfig(t, workspace)
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("[agent]\nvision_model = \"custom/vision-pro\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "diagram.png"), mustBase64(t, tinyPNG), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Controller{controllerDeps: controllerDeps{workspaceRoot: workspace, modelRef: "custom/text-only"}}
	reads := countConfigReads(t)

	ctx, images := c.withTurnImages(context.Background(), "inspect @diagram.png")

	if images.modelReads || len(images.userImages) != 0 || len(images.candidates) != 1 || images.unreadable() != 1 {
		t.Fatalf("text-only parent: modelReads=%v user=%d candidates=%d unreadable=%d", images.modelReads, len(images.userImages), len(images.candidates), images.unreadable())
	}
	if got := agent.VisionRefFor(ctx, "custom/text-only"); got != "custom/vision-pro" {
		t.Fatalf("a text-only child with an attachment routes to %q, want the vision reader", got)
	}
	if *reads == 0 {
		t.Fatal("an attachment decided the model and the reader without reading the config")
	}
}
