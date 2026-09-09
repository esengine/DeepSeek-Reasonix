package agent

import (
	"context"
	"fmt"

	"reasonix/internal/imageinput"
	"reasonix/internal/provider"
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
func (a *Agent) processToolImages(ctx context.Context, text string, images []string) imageResult {
	if len(images) == 0 {
		return imageResult{text: text}
	}
	if a.imageInput.native {
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
