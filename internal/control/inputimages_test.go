package control

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/attachment"
	"reasonix/internal/config"
	"reasonix/internal/provider"
	"reasonix/internal/sessioninbox"
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
	urls := (&Controller{workspaceRoot: dir, selection: modelSelection{ref: "custom/vision-pro"}}).inputImages("look at @" + ref)
	if len(urls) != 1 {
		t.Fatalf("inputImages = %v, want one resolved data URL", urls)
	}
	if !strings.HasPrefix(urls[0], "data:image/png;base64,") {
		t.Errorf("resolved url = %q, want a png data URL", urls[0])
	}
}

func TestControllerInputImagesIgnoresNonAttachmentRefs(t *testing.T) {
	t.Chdir(t.TempDir())
	if urls := newOwnedTestController(t, Options{}).inputImages("plain text with @missing.png"); len(urls) != 0 {
		t.Errorf("inputImages = %v, want none for a non-existent / non-attachment ref", urls)
	}
}

func TestDetectRefsOnlyKeepsMissingImageAttachments(t *testing.T) {
	c := &Controller{workspaceRoot: t.TempDir()}
	refs := c.detectRefs("inspect @.reasonix/attachments/missing.png and @.reasonix/attachments/missing.pdf")
	if len(refs) != 1 || refs[0].kind != refImage || refs[0].path != ".reasonix/attachments/missing.png" {
		t.Fatalf("refs = %+v, want only the missing image attachment", refs)
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

	urls := (&Controller{workspaceRoot: workspace, selection: modelSelection{ref: "custom/vision-pro"}}).inputImages("look at @docs/diagram.png")
	if len(urls) != 1 {
		t.Fatalf("inputImages = %v, want one resolved data URL", urls)
	}
	if !strings.HasPrefix(urls[0], "data:image/png;base64,") {
		t.Errorf("resolved url = %q, want a png data URL", urls[0])
	}
}

func TestControllerInputImagesResolvesAttachmentOutsideProcessCWD(t *testing.T) {
	workspace := t.TempDir()
	processDir := t.TempDir()
	writeVisionTestConfig(t, workspace)
	path, err := SaveImageBytesInRoot(workspace, "image/png", mustBase64(t, tinyPNG))
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(processDir)
	c := &Controller{workspaceRoot: workspace, selection: modelSelection{ref: "custom/vision-pro"}}
	urls := c.inputImages("look at @" + filepath.ToSlash(path))
	if len(urls) != 1 || !strings.HasPrefix(urls[0], "data:image/png;base64,") {
		t.Fatalf("inputImages = %v, want one workspace-owned image", urls)
	}
}

func TestSubmitIdentifiedRejectsMissingExplicitImageBeforeRunner(t *testing.T) {
	workspace := t.TempDir()
	runner := &recordingSessionRunner{session: agent.NewSession("sys")}
	c := newOwnedTestController(t, Options{WorkspaceRoot: workspace, Runner: runner})
	_, err := c.SubmitIdentified(SubmissionRequest{
		Input: "inspect @.reasonix/attachments/missing.png", Display: "inspect image",
	})
	var failures ImageReferenceFailures
	if !errors.As(err, &failures) || len(failures) != 1 || failures[0].Code != ImageReferenceMissing {
		t.Fatalf("error = %#v, want one missing image failure", err)
	}
	if len(runner.inputs) != 0 {
		t.Fatalf("runner inputs = %v, want no model call", runner.inputs)
	}
}

func TestDirectAndEditedSubmissionsRejectMissingExplicitImageBeforeRunner(t *testing.T) {
	const input = "inspect @.reasonix/attachments/missing.png"
	for _, tc := range []struct {
		name   string
		submit func(*Controller)
	}{
		{name: "direct", submit: func(c *Controller) { c.SubmitDisplay("inspect image", input) }},
		{name: "edited", submit: func(c *Controller) { c.SubmitEditedDisplay("inspect image", input, "old prompt") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &recordingSessionRunner{session: agent.NewSession("sys")}
			c := newOwnedTestController(t, Options{WorkspaceRoot: t.TempDir(), Runner: runner})
			tc.submit(c)
			if len(runner.inputs) != 0 {
				t.Fatalf("runner inputs = %v, want no model call", runner.inputs)
			}
		})
	}
}

func TestPreparedAttachmentSurvivesWorkspaceFileDeletion(t *testing.T) {
	workspace := t.TempDir()
	ref, err := SaveImageBytesInRoot(workspace, "image/png", mustBase64(t, tinyPNG))
	if err != nil {
		t.Fatal(err)
	}
	c := newOwnedTestController(t, Options{WorkspaceRoot: workspace})
	prepared, failures := c.prepareExplicitImageReferences("inspect @" + filepath.ToSlash(ref))
	if len(failures) != 0 || len(prepared.inputs) != 1 {
		t.Fatalf("prepared = %+v failures = %v", prepared, failures)
	}
	if err := os.Remove(filepath.Join(workspace, filepath.FromSlash(ref))); err != nil {
		t.Fatal(err)
	}
	raw, err := c.attachmentService().ReadVerified(t.Context(), *prepared.inputs[0].Attachment)
	if err != nil {
		t.Fatalf("persisted original should survive workspace deletion: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("persisted original was empty")
	}
	variant, err := c.attachmentService().PrepareVariant(t.Context(), *prepared.inputs[0].Attachment, attachment.VariantPolicyV1)
	if err != nil {
		t.Fatalf("variant rebuild after workspace deletion: %v", err)
	}
	if len(variant.Bytes) == 0 {
		t.Fatal("rebuilt variant was empty")
	}
}

func TestExplicitImagePreparationIsIndependentOfToolApprovalMode(t *testing.T) {
	workspace := t.TempDir()
	ref, err := SaveImageBytesInRoot(workspace, "image/png", mustBase64(t, tinyPNG))
	if err != nil {
		t.Fatal(err)
	}
	var want string
	for _, mode := range []string{ToolApprovalWorkspaceWrite, ToolApprovalDangerFullAccess} {
		t.Run(mode, func(t *testing.T) {
			c := newOwnedTestController(t, Options{WorkspaceRoot: workspace})
			c.SetToolApprovalMode(mode)
			prepared, failures := c.prepareExplicitImageReferences("inspect @" + filepath.ToSlash(ref))
			if len(failures) != 0 || len(prepared.ordered) != 1 {
				t.Fatalf("prepared images = %v, failures = %v; want one frozen image", prepared.ordered, failures)
			}
			got := prepared.ordered[0]
			if want == "" {
				want = got
			} else if got != want {
				t.Fatal("permission profiles produced different image inputs")
			}
		})
	}
}

func TestSubmitIdentifiedRejectsAllImagesWhenOneIsMissing(t *testing.T) {
	workspace := t.TempDir()
	valid, err := SaveImageBytesInRoot(workspace, "image/png", mustBase64(t, tinyPNG))
	if err != nil {
		t.Fatal(err)
	}
	runner := &recordingSessionRunner{session: agent.NewSession("sys")}
	c := newOwnedTestController(t, Options{WorkspaceRoot: workspace, Runner: runner})
	_, err = c.SubmitIdentified(SubmissionRequest{Input: "inspect @" + filepath.ToSlash(valid) + " @.reasonix/attachments/missing.png"})
	var failures ImageReferenceFailures
	if !errors.As(err, &failures) || len(failures) != 1 {
		t.Fatalf("error = %#v, want partial-set rejection", err)
	}
	if len(runner.inputs) != 0 {
		t.Fatalf("runner inputs = %v, want atomic rejection", runner.inputs)
	}
}

func TestSubmitIdentifiedClassifiesExplicitImageFailuresBeforeRunner(t *testing.T) {
	workspace := t.TempDir()
	attachments := filepath.Join(workspace, ".reasonix", "attachments")
	if err := os.MkdirAll(attachments, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(attachments, "corrupt.png"), []byte("not an image"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".reasonix", "outside.png"), mustBase64(t, tinyPNG), 0o644); err != nil {
		t.Fatal(err)
	}
	tooLarge := filepath.Join(attachments, "too-large.png")
	if err := os.WriteFile(tooLarge, []byte("\x89PNG\r\n\x1a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(tooLarge, maxImageAttachmentBytes+1); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(attachments, "link.png")
	symlinkAvailable := os.Symlink(filepath.Join(workspace, ".reasonix", "outside.png"), linkPath) == nil

	cases := []struct {
		name string
		ref  string
		code ImageReferenceFailureCode
	}{
		{name: "corrupt", ref: ".reasonix/attachments/corrupt.png", code: ImageReferenceUnsupported},
		{name: "traversal", ref: ".reasonix/attachments/../outside.png", code: ImageReferenceUnsafe},
		{name: "too large", ref: ".reasonix/attachments/too-large.png", code: ImageReferenceTooLarge},
	}
	if symlinkAvailable {
		cases = append(cases, struct {
			name string
			ref  string
			code ImageReferenceFailureCode
		}{name: "symlink", ref: ".reasonix/attachments/link.png", code: ImageReferenceUnsafe})
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner := &recordingSessionRunner{session: agent.NewSession("sys")}
			c := newOwnedTestController(t, Options{WorkspaceRoot: workspace, Runner: runner})
			_, err := c.SubmitIdentified(SubmissionRequest{Input: "inspect @" + tc.ref})
			var failures ImageReferenceFailures
			if !errors.As(err, &failures) || len(failures) != 1 || failures[0].Code != tc.code {
				t.Fatalf("error = %#v, want one %s failure", err, tc.code)
			}
			if len(runner.inputs) != 0 {
				t.Fatalf("runner inputs = %v, want no model call", runner.inputs)
			}
		})
	}
}

func TestSubmitIdentifiedRejectsInvocationImageBeforePreparingInvocation(t *testing.T) {
	workspace := t.TempDir()
	runner := &recordingSessionRunner{session: agent.NewSession("sys")}
	c := newOwnedTestController(t, Options{WorkspaceRoot: workspace, Runner: runner})
	_, err := c.SubmitIdentified(SubmissionRequest{
		Input:       "inspect @.reasonix/attachments/missing.png",
		Invocations: []InvocationRequest{{Name: "missing-skill", Kind: "skill"}},
	})
	var failures ImageReferenceFailures
	if !errors.As(err, &failures) || len(failures) != 1 || failures[0].Code != ImageReferenceMissing {
		t.Fatalf("error = %#v, want image admission failure", err)
	}
	if len(runner.inputs) != 0 {
		t.Fatalf("runner inputs = %v, want no model call", runner.inputs)
	}
}

func TestEnqueueInboxRejectsMissingExplicitImageWithoutDurableItem(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	c := newOwnedTestController(t, Options{
		WorkspaceRoot: dir,
		SessionDir:    dir,
		SessionPath:   sessionPath,
	})
	_, err := c.EnqueueInbox(InboxRequest{Submit: "inspect @.reasonix/attachments/missing.png"})
	var failures ImageReferenceFailures
	if !errors.As(err, &failures) || len(failures) != 1 || failures[0].Code != ImageReferenceMissing {
		t.Fatalf("error = %#v, want missing image failure", err)
	}
	if snap := c.InboxSnapshot(); len(snap.Items) != 0 {
		t.Fatalf("rejected image submission created inbox items: %+v", snap.Items)
	}
}

func TestControllerInputImagesResolvesAbsoluteWorkspaceImage(t *testing.T) {
	workspace := t.TempDir()
	writeVisionTestConfig(t, workspace)
	path := filepath.Join(workspace, "diagram.png")
	if err := os.WriteFile(path, mustBase64(t, tinyPNG), 0o644); err != nil {
		t.Fatal(err)
	}

	urls := (&Controller{workspaceRoot: workspace, selection: modelSelection{ref: "custom/vision-pro"}}).inputImages("look at @" + path)
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

	urls := newOwnedTestController(t, Options{}).inputImages("look at @" + path)
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

	c := &Controller{workspaceRoot: workspace, selection: modelSelection{ref: "custom/text-only"}}
	if urls := c.inputImages("look at @diagram.png"); len(urls) != 0 {
		t.Fatalf("text-only model should suppress image payloads, got %v", urls)
	}

	c.selection.ref = "custom/vision-pro"
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

	c := &Controller{workspaceRoot: workspace, selection: modelSelection{ref: "custom/text-only"}}
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

	c := &Controller{workspaceRoot: workspace, selection: modelSelection{ref: "custom/vision-pro"}}
	userImages, candidates := c.resolveTurnImages("inspect @diagram.png")
	if len(userImages) != 1 || len(candidates) != 1 {
		t.Fatalf("turn images = %v, candidates = %v; want one image in both paths", userImages, candidates)
	}
	if &userImages[0] != &candidates[0] || userImages[0] != candidates[0] {
		t.Fatal("vision parent and subagent candidates should reuse the same resolved image slice")
	}

	c.selection.ref = "custom/text-only"
	userImages, candidates = c.resolveTurnImages("inspect @diagram.png")
	if len(userImages) != 0 || len(candidates) != 1 {
		t.Fatalf("text parent turn images = %v, candidates = %v; want candidates only", userImages, candidates)
	}
}

func TestGoalRoundDoesNotInheritPriorTurnImageCandidates(t *testing.T) {
	workspace := t.TempDir()
	writeVisionTestConfig(t, workspace)
	path := filepath.Join(workspace, "diagram.png")
	if err := os.WriteFile(path, mustBase64(t, tinyPNG), 0o644); err != nil {
		t.Fatal(err)
	}

	c := &Controller{workspaceRoot: workspace, selection: modelSelection{ref: "custom/text-only"}}
	initial := c.prepareOrchestratedTurnImages(orchestratedTurn{
		raw:       "inspect the diagnostic",
		imageRefs: "@diagram.png",
	})
	if len(initial.userImages) != 0 || len(initial.imageCandidates) != 1 {
		t.Fatalf("initial turn images = %v, candidates = %v; want child-only candidate", initial.userImages, initial.imageCandidates)
	}

	ctx := agent.WithSubagentImageCandidates(context.Background(), initial.imageCandidates)
	continuation := orchestratedTurn{goalRound: &goalRoundReservation{}, synthetic: true, raw: "continue the target"}
	userImages, candidates := c.imagesForOrchestratedTurn(ctx, continuation)
	if len(userImages) != 0 || len(candidates) != 0 {
		t.Fatalf("new Goal round inherited prior images = %v, candidates = %v", userImages, candidates)
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

	c := &Controller{workspaceRoot: workspace, selection: modelSelection{ref: "deleted/model"}}
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

	c := &Controller{workspaceRoot: dir, selection: modelSelection{ref: "custom/vision-pro"}}
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

func TestResolveRefsUnreadableImageDoesNotClaimAttached(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeVisionTestConfig(t, dir)
	const imagePath = ".reasonix/attachments/empty.png"
	if err := os.MkdirAll(filepath.Dir(imagePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(imagePath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	c := &Controller{workspaceRoot: dir, selection: modelSelection{ref: "custom/vision-pro"}}
	block, errs := c.ResolveRefs(t.Context(), "look at @"+imagePath)
	if len(errs) != 1 || (!strings.Contains(errs[0], "between 1 byte and 64 MB") && !strings.Contains(errs[0], "exceeds the allowed size") && !strings.Contains(errs[0], "size_limit")) {
		t.Fatalf("ResolveRefs errors = %v, want unreadable-image error", errs)
	}
	if strings.Contains(block, "attached as visual input") {
		t.Fatalf("unreadable image claimed successful attachment:\n%s", block)
	}
}

func TestFreezeInboxReferencesResolvesLargeImageOnce(t *testing.T) {
	workspace := t.TempDir()
	t.Chdir(workspace)
	cfg := config.Default()
	cfg.DefaultModel = "deepseek/deepseek-v4-flash-vision-exp"
	cfg.Providers = []config.ProviderEntry{{
		Name: "deepseek", Kind: "openai", BaseURL: "https://api.deepseek.com",
		Models: []string{"deepseek-v4-flash-vision-exp"}, VisionModels: []string{"deepseek-v4-flash-vision-exp"},
		APIKeyEnv: "DEEPSEEK_API_KEY",
	}}
	if err := cfg.SaveTo(filepath.Join(workspace, "reasonix.toml")); err != nil {
		t.Fatal(err)
	}
	previousLimit := inlineImageLimit
	inlineImageLimit = 4
	t.Cleanup(func() { inlineImageLimit = previousLimit })
	uploads := 0
	previousUpload := uploadVisionFile
	uploadVisionFile = func(_ context.Context, _ provider.FileUpload) (string, error) {
		uploads++
		return "file-api-shared-resolution", nil
	}
	t.Cleanup(func() { uploadVisionFile = previousUpload })

	imagePath := filepath.Join(workspace, ".reasonix", "attachments", "large.png")
	if err := os.MkdirAll(filepath.Dir(imagePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(imagePath, mustBase64(t, tinyPNG), 0o644); err != nil {
		t.Fatal(err)
	}
	c := newOwnedTestController(t, Options{WorkspaceRoot: workspace})
	c.selection.ref = cfg.DefaultModel
	env := sessioninbox.PromptEnvelope{SubmitText: "look", ExplicitRefs: []string{filepath.ToSlash(filepath.Join(".reasonix", "attachments", "large.png"))}}
	if err := c.freezeInboxEnvelopeReferences(t.Context(), &env, env.SubmitText, env.ExplicitRefs); err != nil {
		t.Fatal(err)
	}
	if len(env.ImageInputs) != 1 || env.ImageInputs[0].Kind != attachment.KindAttachment {
		t.Fatalf("image inputs = %+v, want one persisted attachment", env.ImageInputs)
	}
	if len(env.FrozenImages) != 0 {
		t.Fatalf("frozen images = %v, want none before request preparation", env.FrozenImages)
	}
	if uploads != 0 {
		t.Fatalf("image uploads = %d, want none until request preparation", uploads)
	}
	if !strings.Contains(env.FrozenRefBlock, "attached as visual input") {
		t.Fatalf("successful freeze did not produce the visual-input note:\n%s", env.FrozenRefBlock)
	}
}

func TestControllerInputImagesPassesHTTPURLAndFileID(t *testing.T) {
	workspace := t.TempDir()
	writeVisionTestConfig(t, workspace)
	c := &Controller{workspaceRoot: workspace, selection: modelSelection{ref: "custom/vision-pro"}}
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
	c := &Controller{workspaceRoot: workspace, selection: modelSelection{ref: "deepseek/deepseek-v4-flash-vision-exp"}}
	got := c.inputImages("look at @.reasonix/attachments/big.png")
	if len(got) != 1 || got[0] != "file-api-uploaded0001" {
		t.Fatalf("inputImages = %v, want uploaded file_id", got)
	}
}
