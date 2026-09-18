package agent

import (
	"context"
	"fmt"
	"strings"

	"reasonix/internal/attachment"
	"reasonix/internal/provider"
)

func (a *Agent) preflightRequestImages(ctx context.Context, messages []provider.Message) ([]provider.Message, error) {
	identity := provider.ResolveImageRequestIdentity(a.svc.prov, a.modelRef)
	decisions := requestImageIsolationDecisions(messages)
	applicable := make(map[string]bool, len(decisions))
	for _, decision := range decisions {
		if decision.Scope.Matches(identity) {
			applicable[provider.ImageDecisionKey(decision)] = true
		}
	}
	out := append([]provider.Message(nil), messages...)
	for messageIndex := range out {
		if len(out[messageIndex].Images) == 0 {
			continue
		}
		out[messageIndex].Images = append([]string(nil), out[messageIndex].Images...)
		for ordinal, image := range out[messageIndex].Images {
			imageIdentity := provider.ImageIdentity{
				MessageID: out[messageIndex].ID, ImageOrdinal: ordinal,
				ContentDigest: provider.ImageContentDigest(image),
			}
			if imageIsolationAlreadyApplies(decisions, identity, imageIdentity) {
				continue
			}
			reason, replacement := locallyRejectedImage(ctx, image)
			if replacement != "" {
				out[messageIndex].Images[ordinal] = replacement
				continue
			}
			if reason == "" {
				continue
			}
			if strings.TrimSpace(imageIdentity.MessageID) == "" {
				return nil, fmt.Errorf("image preflight: invalid image has no stable message identity")
			}
			decision := provider.ImageIsolationDecision{
				Version: 1, Identity: imageIdentity, Reason: reason,
				Scope: provider.ImageIsolationScope{Kind: "content"},
			}
			key := provider.ImageDecisionKey(decision)
			if applicable[key] {
				continue
			}
			if recorder, ok := a.svc.sessionCheckpointer.(SessionImageIsolationRecorder); ok {
				if err := recorder.RecordSessionImageIsolation(ctx, decision); err != nil {
					return nil, fmt.Errorf("persist image isolation: %w", err)
				}
			}
			decisions = append(decisions, decision)
			applicable[key] = true
		}
	}
	projected, _ := provider.ApplyImageIsolation(out, decisions, identity)
	return projected, nil
}

func requestImageIsolationDecisions(messages []provider.Message) []provider.ImageIsolationDecision {
	var decisions []provider.ImageIsolationDecision
	seen := map[string]bool{}
	for _, message := range messages {
		for _, decision := range message.ImageIsolations {
			key := provider.ImageDecisionKey(decision)
			if !seen[key] {
				seen[key] = true
				decisions = append(decisions, decision)
			}
		}
	}
	return decisions
}

func imageIsolationAlreadyApplies(decisions []provider.ImageIsolationDecision, request provider.ImageRequestIdentity, identity provider.ImageIdentity) bool {
	for _, decision := range decisions {
		if decision.Version == 1 && decision.Scope.Matches(request) && decision.Identity == identity {
			return true
		}
	}
	return false
}

func locallyRejectedImage(ctx context.Context, image string) (reason, replacement string) {
	trimmed := strings.TrimSpace(image)
	if trimmed == "" {
		return "empty_image", ""
	}
	if !strings.HasPrefix(trimmed, "data:") {
		return "", ""
	}
	mime, raw, err := attachment.ParseDataURL(trimmed)
	if err != nil {
		switch {
		case attachment.Is(err, attachment.CodeSize):
			return "image_size_limit", ""
		case attachment.Is(err, attachment.CodeCorrupt):
			return "corrupt_image", ""
		default:
			return "invalid_image_data_url", ""
		}
	}
	_, _, _, err = attachment.ValidateImage(raw, mime, attachment.DefaultPolicy())
	if err != nil {
		switch {
		case attachment.Is(err, attachment.CodeSize):
			return "image_size_limit", ""
		case attachment.Is(err, attachment.CodeCorrupt):
			return "corrupt_image", ""
		default:
			return "unsupported_image_format", ""
		}
	}
	if len(raw) <= provider.MaxInlineImageBytes {
		return "", ""
	}
	variant, err := attachment.PrepareInlineVariant(ctx, raw, mime)
	if err != nil || len(variant.Bytes) == 0 || len(variant.Bytes) > provider.MaxInlineImageBytes {
		return "image_size_limit", ""
	}
	return "", attachment.DataURL(variant.MIME, variant.Bytes)
}
