package control

import (
	"context"
	"errors"
	"path/filepath"
	"reasonix/internal/agent"
	"reasonix/internal/attachment"
	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/sessioninbox"
	"reasonix/internal/tool"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAttachmentUploadUsesRequestRouteAndCancellation(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.DefaultModel = "main/text"
	cfg.Providers = []config.ProviderEntry{
		{Name: "main", Kind: "openai", BaseURL: "https://api.deepseek.com", Models: []string{"text"}},
		{Name: "vision", Kind: "anthropic", BaseURL: "https://api.deepseek.com/anthropic", Models: []string{"vision"}},
		{Name: "external", Kind: "openai", BaseURL: "https://vision.example.invalid/v1", Models: []string{"vision"}},
	}
	c := newOwnedTestController(t, Options{WorkspaceRoot: root, ModelRef: "main/text", ImageRouteConfig: cfg})
	d, err := c.StageImage(t.Context(), "shot.png", "image/png", "data:image/png;base64,"+tinyPNG)
	if err != nil {
		t.Fatal(err)
	}
	oldLimit, oldUpload := inlineImageLimit, uploadVisionFile
	t.Cleanup(func() { inlineImageLimit = oldLimit; uploadVisionFile = oldUpload })
	inlineImageLimit = 1
	var uploads []provider.FileUpload
	uploadVisionFile = func(_ context.Context, r provider.FileUpload) (string, error) {
		uploads = append(uploads, r)
		return "file-route", nil
	}
	msgs := []provider.Message{{Role: provider.RoleUser, Content: "image", ImageInputs: c.attachmentService().InputsFromRefs([]attachment.AttachmentRef{d.Ref})}}
	if _, err := c.ResolveRequestImagesForModel(t.Context(), msgs, "vision/vision", true); err != nil {
		t.Fatal(err)
	}
	if len(uploads) != 1 || uploads[0].BaseURL != cfg.Providers[1].BaseURL || uploads[0].Protocol != "anthropic" {
		t.Fatalf("wrong route: %+v", uploads)
	}
	if _, err := c.ResolveRequestImagesForModel(t.Context(), msgs, "external/vision", true); err != nil {
		t.Fatal(err)
	}
	if len(uploads) != 1 {
		t.Fatal("external route uploaded to main provider")
	}
	uploadVisionFile = func(context.Context, provider.FileUpload) (string, error) { return "", context.Canceled }
	if _, err := c.ResolveRequestImagesForModel(t.Context(), msgs, "vision/vision", true); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel fell back inline: %v", err)
	}
}

type reviewImageProvider struct {
	requests chan provider.Request
	textOnly bool
}

func (p *reviewImageProvider) Name() string { return "review" }
func (p *reviewImageProvider) ModelInfo() provider.ModelInfo {
	if p.textOnly {
		return provider.ModelInfo{InputModalities: []provider.ModelModality{provider.ModalityText}}
	}
	return provider.ModelInfo{InputModalities: []provider.ModelModality{provider.ModalityText, provider.ModalityImage}}
}

func TestAttachmentTextModelUsesVisionProvider(t *testing.T) {
	root := t.TempDir()
	writeVisionTestConfig(t, root)
	main := &reviewImageProvider{requests: make(chan provider.Request, 4), textOnly: true}
	vision := &reviewImageProvider{requests: make(chan provider.Request, 4)}
	ag := agent.New(main, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{ModelRef: "custom/text"}, event.Discard)
	native := false
	c := newOwnedTestController(t, Options{WorkspaceRoot: root, Runner: ag, Executor: ag, ModelRef: "custom/text", FrozenImageInput: &native, VisionModel: "custom/vision-pro", VisionProviderResolver: func(string) (provider.Provider, error) { return vision, nil }})
	draft, err := c.StageImage(t.Context(), "shot.png", "image/png", "data:image/png;base64,"+tinyPNG)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.SubmitIdentified(SubmissionRequest{Input: "inspect", Attachments: []SubmissionAttachment{{ClientAttachmentID: "image-1", DraftID: draft.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case req := <-vision.requests:
		if len(req.Messages) != 1 || len(req.Messages[0].Images) != 1 {
			t.Fatalf("vision request has no image: %+v", req.Messages)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("vision provider did not receive image")
	}
	select {
	case req := <-main.requests:
		for _, msg := range req.Messages {
			if len(msg.Images) != 0 || len(msg.ImageInputs) != 0 {
				t.Fatal("text model received raw image")
			}
		}
	case <-time.After(10 * time.Second):
		t.Fatal("text provider did not receive summary")
	}
}

func TestAttachmentRetryAfterDraftRelease(t *testing.T) {
	root := t.TempDir()
	c := newOwnedTestController(t, Options{WorkspaceRoot: root, SessionPath: filepath.Join(root, "session.jsonl"), Sink: event.Discard})
	d, err := c.StageImage(t.Context(), "shot.png", "image/png", "data:image/png;base64,"+tinyPNG)
	if err != nil {
		t.Fatal(err)
	}
	req := SubmissionRequest{ID: "durable-image", Input: "inspect", Attachments: []SubmissionAttachment{{ClientAttachmentID: "stable-image", DraftID: d.ID}}}
	first, err := c.submitIdentified(req, func() {
		if err := c.prepareTurnAdmission(func(context.Context) error { return nil })(t.Context()); err != nil {
			t.Fatal(err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	c.ReleaseDraftImage(d.ID)
	second, err := c.SubmitIdentified(req)
	if err != nil || first != second {
		t.Fatalf("retry = %+v, %v; want %+v", second, err, first)
	}
}
func (p *reviewImageProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.requests <- req
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestAttachmentRegressionDesktopImageReachesProvider(t *testing.T) {
	for _, mode := range []string{"draft", "legacy"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			writeVisionTestConfig(t, root)
			p := &reviewImageProvider{requests: make(chan provider.Request, 4)}
			ag := agent.New(p, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, event.Discard)
			native := true
			c := newOwnedTestController(t, Options{WorkspaceRoot: root, Runner: ag, Executor: ag, ModelRef: "custom/vision-pro", FrozenImageInput: &native})
			input := "inspect "
			if mode == "draft" {
				d, err := c.StageImage(t.Context(), "shot.png", "image/png", "data:image/png;base64,"+tinyPNG)
				if err != nil {
					t.Fatal(err)
				}
				input += "@draft:" + d.ID
			} else {
				ref, err := SaveImageDataURLInRoot(root, "data:image/png;base64,"+tinyPNG)
				if err != nil {
					t.Fatal(err)
				}
				input += "@" + ref
			}
			if _, err := c.SubmitIdentified(SubmissionRequest{Input: input}); err != nil {
				t.Fatal(err)
			}
			select {
			case req := <-p.requests:
				var imgs []string
				for _, m := range req.Messages {
					if m.Role == provider.RoleUser {
						imgs = append(imgs, m.Images...)
					}
				}
				if len(imgs) != 1 || !strings.HasPrefix(imgs[0], "data:image/png;base64,") {
					t.Fatalf("provider image inputs = %q, expected one resolved data URL", imgs)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("provider did not start")
			}
		})
	}
}

func TestAttachmentRegressionQueuePreservesDraft(t *testing.T) {
	c := newOwnedTestController(t, Options{WorkspaceRoot: t.TempDir()})
	d, err := c.StageImage(t.Context(), "shot.png", "image/png", "data:image/png;base64,"+tinyPNG)
	if err != nil {
		t.Fatal(err)
	}
	var env sessioninbox.PromptEnvelope
	if err := c.freezeInboxEnvelopeReferences(t.Context(), &env, "inspect @draft:"+d.ID, nil); err != nil {
		t.Fatal(err)
	}
	if len(env.ImageInputs) != 1 {
		t.Fatalf("queued image inputs = %d, want 1", len(env.ImageInputs))
	}
}

func TestAttachmentRegressionDraftAdmissionLimitsAndDedup(t *testing.T) {
	c := newOwnedTestController(t, Options{WorkspaceRoot: t.TempDir()})
	d, err := c.StageImage(t.Context(), "shot.png", "image/png", "data:image/png;base64,"+tinyPNG)
	if err != nil {
		t.Fatal(err)
	}
	p, fail := c.prepareSubmissionImages(SubmissionRequest{Input: "inspect @draft:" + d.ID, DraftIDs: []string{d.ID}})
	if len(fail) > 0 {
		t.Fatal(fail)
	}
	if len(p.inputs) != 1 {
		t.Errorf("one UI draft resolved to %d images", len(p.inputs))
	}
	ids := make([]string, 21)
	for i := range ids {
		x, err := c.StageImage(t.Context(), "shot.png", "image/png", "data:image/png;base64,"+tinyPNG)
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = x.ID
	}
	_, fail = c.prepareSubmissionImages(SubmissionRequest{Input: "inspect", DraftIDs: ids})
	if len(fail) == 0 {
		t.Error("21 image drafts admitted despite max 20 policy")
	}
}

func TestAttachmentRegressionDraftAndLegacyAreBothPrepared(t *testing.T) {
	root := t.TempDir()
	c := newOwnedTestController(t, Options{WorkspaceRoot: root})
	d, err := c.StageImage(t.Context(), "shot.png", "image/png", "data:image/png;base64,"+tinyPNG)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := SaveImageDataURLInRoot(root, "data:image/png;base64,"+tinyPNG)
	if err != nil {
		t.Fatal(err)
	}
	p, fail := c.prepareSubmissionImages(SubmissionRequest{Input: "inspect @draft:" + d.ID + " @" + ref})
	if len(fail) > 0 {
		t.Fatal(fail)
	}
	if len(p.inputs) != 2 {
		t.Fatalf("mixed image inputs = %d, want 2", len(p.inputs))
	}
}

func TestAttachmentRegressionAttachmentReceiptLookup(t *testing.T) {
	root := t.TempDir()
	c := newOwnedTestController(t, Options{WorkspaceRoot: root, SessionPath: filepath.Join(root, "session.jsonl"), Sink: event.Discard})
	d, err := c.StageImage(t.Context(), "shot.png", "image/png", "data:image/png;base64,"+tinyPNG)
	if err != nil {
		t.Fatal(err)
	}
	req := SubmissionRequest{ID: "with-image", Input: "inspect @draft:" + d.ID, DraftIDs: []string{d.ID}}
	_, err = c.submitIdentified(req, func() {
		if err := c.prepareTurnAdmission(func(context.Context) error { return nil })(context.Background()); err != nil {
			t.Fatal(err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	_, found, err := c.LookupSubmission(req)
	if err != nil || !found {
		t.Fatalf("same submission lookup: found=%v err=%v", found, err)
	}
}

func TestAttachmentRegressionConcurrentDraftStaging(t *testing.T) {
	c := newOwnedTestController(t, Options{WorkspaceRoot: t.TempDir()})
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			<-start
			_, err := c.StageImage(t.Context(), "shot.png", "image/png", "data:image/png;base64,"+tinyPNG)
			if err != nil {
				t.Error(err)
			}
		})
	}
	close(start)
	wg.Wait()
}
