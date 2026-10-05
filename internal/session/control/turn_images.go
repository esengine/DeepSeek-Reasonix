package control

import (
	"context"
	"fmt"
	"strings"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/config"
	"reasonix/internal/runtime/agent"
)

// turnImages is what became of this turn's attachments: what the model gets,
// what a delegate could get, and what nothing gets. One value answers all three,
// so a resolved turn is a non-nil pointer rather than a separate flag.
type turnImages struct {
	userImages []string
	candidates []string
	skipped    []error
	// modelReads is the model's capability, which a turn carrying no pixels of
	// its own (a Goal continuation) still has.
	modelReads bool
}

// unreadable counts what the user attached and this turn's own model cannot see.
func (t *turnImages) unreadable() int {
	if t == nil || t.modelReads {
		return 0
	}
	return len(t.candidates)
}

// received counts the images this turn's own message carries to the model.
func (t *turnImages) received() int {
	if t == nil {
		return 0
	}
	return len(t.userImages)
}

func (t *turnImages) reasons() string {
	parts := make([]string, 0, len(t.skipped))
	for _, err := range t.skipped {
		parts = append(parts, err.Error())
	}
	return strings.Join(parts, "; ")
}

// resolveTurnImages resolves each user attachment once. Text-only parents keep
// the data only as candidates for a vision-capable child; vision-capable
// parents reuse the same data URLs for their own provider request.
func (c *Controller) resolveTurnImages(line string) *turnImages {
	candidates, skipped := c.resolveInputImageCandidates(line)
	images := &turnImages{candidates: candidates, skipped: skipped, modelReads: c.imageInputEnabled()}
	if images.modelReads {
		images.userImages = candidates
	}
	return images
}

// prepareOrchestratedTurnImages resolves the turn's image references, leaving an
// already-pinned set alone: a replay pins its images so paths that changed since
// are not read a second time.
func (c *Controller) prepareOrchestratedTurnImages(turn orchestratedTurn) orchestratedTurn {
	if turn.images == nil {
		turn.images = c.resolveTurnImages(turn.imageReferenceInput())
	}
	return turn
}

// frozenTurnImages pins an already-resolved image set to a turn.
func (c *Controller) frozenTurnImages(images []string) *turnImages {
	frozen := &turnImages{candidates: append([]string(nil), images...), modelReads: c.imageInputEnabled()}
	if frozen.modelReads {
		frozen.userImages = append([]string(nil), images...)
	}
	return frozen
}

func (c *Controller) imagesForOrchestratedTurn(ctx context.Context, turn orchestratedTurn) *turnImages {
	if turn.images != nil {
		return turn.images
	}
	if turn.goalContinuation != nil {
		// A Goal continuation belongs to the same visible user turn, so keep its
		// child-only image candidates. Do not add them to the synthetic parent
		// message: a vision parent already has the image in its earlier history.
		return &turnImages{candidates: agent.SubagentImageCandidates(ctx), modelReads: c.imageInputEnabled()}
	}
	return c.resolveTurnImages(turn.imageReferenceInput())
}

// withTurnImages binds this turn's attachments and hands back what became of
// them, so every entry point composes its input through imageRoutingPrefix
// rather than each deciding on its own what the model is told.
func (c *Controller) withTurnImages(ctx context.Context, line string) (context.Context, *turnImages) {
	images := c.resolveTurnImages(line)
	ctx = agent.WithUserImages(ctx, images.userImages)
	return c.withVisionRouting(agent.WithSubagentImageCandidates(ctx, images.candidates)), images
}

// withVisionRouting names the model that reads an attachment this turn's own
// model cannot. Without it the candidates reach whichever sub-agent runs next,
// whose model was chosen for cost or speed and usually drops them on the wire.
func (c *Controller) withVisionRouting(ctx context.Context) context.Context {
	cfg, err := config.LoadForRoot(c.workspaceRoot)
	if err != nil || cfg == nil || strings.TrimSpace(cfg.Agent.VisionModel) == "" {
		return ctx
	}
	return agent.WithVisionRouting(ctx, cfg.Agent.VisionModel, func(child string) bool {
		if strings.TrimSpace(child) == "" {
			child = c.modelRef
		}
		entry, ok := cfg.ResolveModel(child)
		return ok && config.EffectiveVision(entry)
	})
}

func (turn orchestratedTurn) imageReferenceInput() string {
	if strings.TrimSpace(turn.imageRefs) != "" {
		return turn.imageRefs
	}
	return turn.raw
}

// ImageRoutingTag wraps the note telling the model about images it cannot read;
// without it the model answers as though nothing was attached. It rides the turn
// tail, never the cache-stable prefix, the same way memory and job notes do.
// Exported so a frontend's tests can assert the note arrived without copying its
// wording, which is this package's to change.
const ImageRoutingTag = "attached-images"

// receivedImagesNote is the reference block's other answer: the pictures are in
// this message. A block that defers to a note that never arrives reads as the
// image not having arrived, and a model reasoning at length answers that way.
func receivedImagesNote(n int) string {
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf(
		"<%s>\nThe user attached %d image(s), and they are in this message as images. Look at them directly and answer from what they show.\n"+
			"Each image block in the text only marks where one was referenced. Do not open the files with tools: read_file reads text and will refuse.\n</%s>\n\n",
		ImageRoutingTag, n, ImageRoutingTag)
}

// imageRoutingNote is the only place that tells the model what to do about an
// attachment it cannot read. The reference block beside the image states facts
// and nothing else, because two blocks proposing different routes is how a turn
// ends with the model writing its own OCR script while the configured vision
// model sits unused.
func (c *Controller) imageRoutingNote(n int) string {
	if n <= 0 {
		return ""
	}
	reader := c.visionModelRef()
	if reader == "" {
		return fmt.Sprintf(
			"<%s>\nThe user attached %d image(s). This model cannot read images, so they are not in your context, "+
				"and no vision model is configured to read them for you.\n"+
				"Say so plainly, or use an OCR/image tool if one is available for the local path. "+
				"Never answer as if no image was attached, and never guess what it shows.\n</%s>\n\n",
			ImageRoutingTag, n, ImageRoutingTag)
	}
	return fmt.Sprintf(
		"<%s>\nThe user attached %d image(s). This model cannot read images, so they are not in your context.\n"+
			"Delegate with read_only_task: the attachments are handed to %s, which reads them, automatically.\n"+
			"Do not OCR them yourself and do not write a script to do it — that path is configured and working. "+
			"Never answer as if no image was attached, and never guess what it shows.\n</%s>\n\n",
		ImageRoutingTag, n, reader, ImageRoutingTag)
}

// visionModelRef is the model configured to read what this one cannot.
func (c *Controller) visionModelRef() string {
	cfg, err := config.LoadForRoot(c.workspaceRoot)
	if err != nil || cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Agent.VisionModel)
}

// bindOrchestratedTurnImages resolves the turn's attachments, binds them both
// for the model and for any delegate, and reports how many the model itself
// cannot read.
func (c *Controller) bindOrchestratedTurnImages(ctx context.Context, turn orchestratedTurn) (context.Context, *turnImages) {
	images := c.imagesForOrchestratedTurn(ctx, turn)
	ctx = agent.WithUserImages(ctx, images.userImages)
	ctx = agent.WithSubagentImageCandidates(ctx, images.candidates)
	return ctx, images
}

// imageRoutingPrefix also tells the user where their attachment went. Silence is
// the failure that matters: a pasted screenshot that reaches nothing reads as
// the product ignoring it.
func (c *Controller) imageRoutingPrefix(images *turnImages) string {
	c.noticeUnfitImages(images)
	note := c.unfitImagesNote(images) + receivedImagesNote(images.received())
	if unreadable := images.unreadable(); unreadable > 0 {
		c.notice(c.imagesNotReadableNotice(unreadable))
		note += c.imageRoutingNote(unreadable)
	}
	return note
}

// noticeUnfitImages names an attachment that never left the machine. A host
// rejects an oversized image as an unsupported *format*, and the rejected
// message then fails every later turn — so the reason is said here instead.
func (c *Controller) noticeUnfitImages(images *turnImages) {
	if images == nil || len(images.skipped) == 0 {
		return
	}
	c.notice(fmt.Sprintf(i18n.M.ImagesUnfit, len(images.skipped), images.reasons()))
}

// unfitImagesNote tells the model an attachment exists that it will never see,
// so it does not answer as though nothing was attached.
func (c *Controller) unfitImagesNote(images *turnImages) string {
	if images == nil || len(images.skipped) == 0 {
		return ""
	}
	return fmt.Sprintf(
		"<%s>\nThe user attached %d image(s) that could not be sent to any model: %s\n"+
			"Say so plainly and name what would fix it. Never answer as if nothing was attached, "+
			"and never guess what the image shows.\n</%s>\n\n",
		ImageRoutingTag, len(images.skipped), images.reasons(), ImageRoutingTag)
}

// imagesNotReadableNotice says where the attachment actually went. Announcing a
// delegated read while no vision role is set is the one thing this must not do:
// the picture is dropped before the request, and a user told otherwise goes on
// to ask about what it showed.
func (c *Controller) imagesNotReadableNotice(unreadable int) string {
	if c.visionModelRef() != "" {
		return fmt.Sprintf(i18n.M.ImagesNotReadable, unreadable)
	}
	if offer := c.firstVisionModelRef(); offer != "" {
		return fmt.Sprintf(i18n.M.ImagesNeedVisionRole, unreadable, offer)
	}
	return fmt.Sprintf(i18n.M.ImagesDropped, unreadable)
}

// firstVisionModelRef is a configured model that could have read it.
func (c *Controller) firstVisionModelRef() string {
	cfg, err := config.LoadForRoot(c.workspaceRoot)
	if err != nil || cfg == nil {
		return ""
	}
	return cfg.FirstVisionModelRef()
}
