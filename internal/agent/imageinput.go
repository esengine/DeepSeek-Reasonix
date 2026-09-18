package agent

import (
	"context"
	"fmt"

	"reasonix/internal/attachment"
	"reasonix/internal/imageinput"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type agentImageInput struct {
	service *imageinput.Service
	native  bool
}
type imageResult struct {
	text       string
	summary    *provider.VisionSummary
	diagnostic error
}

func newImageInput(cfg *imageinput.Config, p provider.Provider) agentImageInput {
	if cfg == nil {
		return agentImageInput{native: supportsNativeImages(p)}
	}
	return agentImageInput{service: imageinput.New(*cfg), native: supportsNativeImages(p)}
}
func (a *Agent) ImageInput() *imageinput.Service { return a.imageInput.service }

// ImageRequestResolver turns durable ImageInputs into provider-visible Images
// on a request copy. It must not write variants or uploads back to history.
type ImageRequestResolver interface {
	ResolveRequestImages(ctx context.Context, msgs []provider.Message) ([]provider.Message, error)
	PersistToolImages(ctx context.Context, images []string) ([]attachment.ImageInput, error)
}

func (a *Agent) SetImageRequestResolver(resolver ImageRequestResolver) {
	if a == nil {
		return
	}
	a.imageResolver = resolver
	if a.svc.tools == nil {
		return
	}
	if item, ok := a.svc.tools.Get(tool.HostTask); ok {
		if task, ok := item.(*TaskTool); ok {
			task.imageResolver = resolver
		}
	}
}
func (a *Agent) processToolImages(ctx context.Context, text string, images []string) imageResult {
	if len(images) == 0 {
		return imageResult{text: text}
	}
	// Durable images are persisted by buildBatchToolResult first. Request
	// assembly then selects the actual native or understanding model route.
	if a.imageInput.native || a.imageResolver != nil {
		return imageResult{text: text}
	}
	summary, err := a.imageInput.service.Understand(ctx, a.modelRef, images, a.Session().Snapshot, a.svc.sink)
	if err != nil {
		return imageResult{text: text + fmt.Sprintf("\n[Image understanding unavailable: %v. The tool already executed; its text result remains valid. Do not claim to have seen the image or repeat the original action to retry image understanding.]", err), diagnostic: err}
	}
	return imageResult{text: imageinput.AppendSummary(text, summary), summary: summary}
}

func supportsNativeImages(p provider.Provider) bool {
	info, ok := p.(provider.ModelInfoProvider)
	return ok && info.ModelInfo().SupportsInput(provider.ModalityImage)
}
