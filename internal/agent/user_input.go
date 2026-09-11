package agent

import (
	"context"

	"reasonix/internal/provider"
)

type rawUserInputKey struct{}
type subagentImageCandidatesKey struct{}
type visionSummaryContextKey struct{}

// WithRawUserInput keeps user-authored text separate from host-rendered turn
// context. Runner implementations can persist the raw text while sending their
// composed input to the provider.
func WithRawUserInput(ctx context.Context, raw string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, rawUserInputKey{}, raw)
}

// RawUserInput returns the host-authenticated raw user text when available.
// Direct Agent callers that do not use a Controller keep their input unchanged.
func RawUserInput(ctx context.Context, fallback string) string {
	if ctx == nil {
		return fallback
	}
	if raw, ok := ctx.Value(rawUserInputKey{}).(string); ok {
		return raw
	}
	return fallback
}

// WithSubagentImageCandidates carries attachment data resolved by the parent
// controller for a child model to opt into when it supports vision. Keeping
// this separate from UserImages avoids changing the parent's provider-visible
// text-only turn while allowing a vision-capable child to receive the same
// authorized attachments.
func WithSubagentImageCandidates(ctx context.Context, images []string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, subagentImageCandidatesKey{}, append([]string(nil), images...))
}

// SubagentImageCandidates returns the parent-resolved attachment data that a
// child provider may use when its own model supports vision.
func SubagentImageCandidates(ctx context.Context) []string {
	if ctx == nil {
		return nil
	}
	images, _ := ctx.Value(subagentImageCandidatesKey{}).([]string)
	return append([]string(nil), images...)
}

// WithVisionSummary carries a hidden image-understanding result into the
// foreground turn. The run loop persists it on the user message while keeping
// RawContent as the user-visible text.
func WithVisionSummary(ctx context.Context, summary *provider.VisionSummary) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if summary == nil {
		return ctx
	}
	cp := *summary
	cp.ImageDigests = append([]string(nil), summary.ImageDigests...)
	return context.WithValue(ctx, visionSummaryContextKey{}, &cp)
}

func VisionSummaryFromContext(ctx context.Context) *provider.VisionSummary {
	if ctx == nil {
		return nil
	}
	summary, _ := ctx.Value(visionSummaryContextKey{}).(*provider.VisionSummary)
	if summary == nil {
		return nil
	}
	cp := *summary
	cp.ImageDigests = append([]string(nil), summary.ImageDigests...)
	return &cp
}
