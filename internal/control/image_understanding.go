package control

import (
	"context"
	"fmt"
	"html"
	"strings"

	"reasonix/internal/event"
	"reasonix/internal/nilutil"
	"reasonix/internal/provider"
)

const imageUnderstandingPrompt = `You are an image understanding sidecar for a coding agent.
Describe the attached image(s) so a text-only main model can use them.
Focus on visible UI text, errors, code, status bars, diagrams, layout, and the user's likely target.
Preserve exact visible strings when useful. Be concise and factual.
Use the user's language when obvious. Do not solve the whole task; only describe visual evidence.`

// ImageUnderstandingRef is one user-referenced image prepared for an optional
// sidecar. DataURL is for provider-native vision; Path/Source/SHA256 let local
// OCR commands emit portable, auditable context without inlining image bytes.
type ImageUnderstandingRef struct {
	Source  string
	Path    string
	DataURL string
	SHA256  string
}

// ImageUnderstanding describes attached user images, producing compact text
// that can be injected into a text-only main turn.
type ImageUnderstanding interface {
	DescribeImages(ctx context.Context, userInput string, images []ImageUnderstandingRef) (string, error)
}

type ProviderImageUnderstanding struct {
	prov    provider.Provider
	pricing *provider.Pricing
	sink    event.Sink
}

func NewProviderImageUnderstanding(prov provider.Provider) *ProviderImageUnderstanding {
	if nilutil.IsNil(prov) {
		return nil
	}
	return &ProviderImageUnderstanding{prov: prov}
}

func NewBillableProviderImageUnderstanding(prov provider.Provider, pricing *provider.Pricing, sink event.Sink) *ProviderImageUnderstanding {
	if nilutil.IsNil(prov) {
		return nil
	}
	if nilutil.IsNil(sink) {
		sink = event.Discard
	}
	return &ProviderImageUnderstanding{prov: prov, pricing: pricing, sink: sink}
}

func (u *ProviderImageUnderstanding) DescribeImages(ctx context.Context, userInput string, images []ImageUnderstandingRef) (string, error) {
	if u == nil || nilutil.IsNil(u.prov) {
		return "", fmt.Errorf("image understanding is not initialized")
	}
	dataURLs := make([]string, 0, len(images))
	for _, img := range images {
		if strings.TrimSpace(img.DataURL) != "" {
			dataURLs = append(dataURLs, img.DataURL)
		}
	}
	if len(dataURLs) == 0 {
		return "", nil
	}
	ch, err := u.prov.Stream(ctx, provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: imageUnderstandingPrompt},
			{Role: provider.RoleUser, Content: imageUnderstandingUserPrompt(userInput, len(dataURLs)), Images: dataURLs},
		},
		Temperature: provider.TemperaturePtr(0),
		MaxTokens:   700,
	})
	if err != nil {
		return "", err
	}

	var text strings.Builder
	var usage *provider.Usage
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			text.WriteString(chunk.Text)
		case provider.ChunkUsage:
			usage = chunk.Usage
		case provider.ChunkError:
			return "", chunk.Err
		}
	}
	if usage != nil && usage.TotalTokens > 0 && !nilutil.IsNil(u.sink) {
		u.sink.Emit(event.Event{Kind: event.Usage, Usage: usage, Pricing: u.pricing, UsageSource: event.UsageSourceVision})
	}
	desc := strings.TrimSpace(text.String())
	if desc == "" {
		return "", nil
	}
	return formatImageUnderstandingBlock(combinedImageUnderstandingSource(images), combinedImageUnderstandingSHA(images), "", desc, "", "", "medium"), nil
}

func imageUnderstandingUserPrompt(userInput string, count int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Image count: %d\n\n", count)
	if strings.TrimSpace(userInput) != "" {
		b.WriteString("User request:\n")
		b.WriteString(userInput)
		b.WriteString("\n\n")
	}
	b.WriteString("Return a compact visual description. If the image is a screenshot, include the most relevant visible text and UI state.")
	return b.String()
}

func formatImageUnderstandingBlock(source, sha, visibleText, uiState, errors, layout, confidence string) string {
	if strings.TrimSpace(confidence) == "" {
		confidence = "medium"
	}
	var b strings.Builder
	b.WriteString(imageUnderstandingOpenTag(source, sha))
	b.WriteString("\n")
	writeImageUnderstandingField(&b, "visible_text", visibleText)
	writeImageUnderstandingField(&b, "ui_state", uiState)
	writeImageUnderstandingField(&b, "errors", errors)
	writeImageUnderstandingField(&b, "layout", layout)
	writeImageUnderstandingField(&b, "confidence", confidence)
	b.WriteString("\n</image-understanding>")
	return b.String()
}

func imageUnderstandingOpenTag(source, sha string) string {
	var b strings.Builder
	b.WriteString(`<image-understanding source="`)
	b.WriteString(html.EscapeString(strings.TrimSpace(source)))
	b.WriteString(`"`)
	if strings.TrimSpace(sha) != "" {
		b.WriteString(` sha256="`)
		b.WriteString(html.EscapeString(strings.TrimSpace(sha)))
		b.WriteString(`"`)
	}
	b.WriteString(">")
	return b.String()
}

func writeImageUnderstandingField(b *strings.Builder, name, value string) {
	value = strings.TrimSpace(value)
	b.WriteString(name)
	b.WriteString(":")
	if value != "" {
		b.WriteString(" ")
		if strings.Contains(value, "\n") {
			b.WriteString("\n")
		}
		b.WriteString(value)
	}
	b.WriteString("\n")
}

func combinedImageUnderstandingSource(images []ImageUnderstandingRef) string {
	var parts []string
	for _, img := range images {
		if s := strings.TrimSpace(img.Source); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ",")
}

func combinedImageUnderstandingSHA(images []ImageUnderstandingRef) string {
	var parts []string
	for _, img := range images {
		if s := strings.TrimSpace(img.SHA256); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ",")
}
