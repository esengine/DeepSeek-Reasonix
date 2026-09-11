package imageinput

import (
	"strings"

	"reasonix/internal/provider"
)

func clone(v *provider.VisionSummary) *provider.VisionSummary {
	cp := *v
	cp.ImageDigests = append([]string(nil), v.ImageDigests...)
	return &cp
}

func (s *Service) lookup(target string, images []string, history func() []provider.Message) ([]string, string, bool, *provider.VisionSummary) {
	digests := make([]string, len(images))
	cacheable := true
	for i, ref := range images {
		digests[i] = ImageDigest(ref)
		if provider.IsImageHTTPURL(ref) {
			cacheable = false
		}
	}
	match := func(v *provider.VisionSummary) bool {
		return v != nil && v.Version == visionSummaryVersion && v.PromptVersion == visionSummaryPromptVersion && v.ModelRef == target && sameImageDigests(v.ImageDigests, digests) && strings.TrimSpace(v.Summary) != ""
	}
	key := target + "|" + strings.Join(digests, "|")
	if cacheable {
		if match(s.cached[key]) {
			return digests, key, cacheable, clone(s.cached[key])
		}
		if history != nil {
			for _, msg := range history() {
				if match(msg.VisionSummary) {
					return digests, key, cacheable, clone(msg.VisionSummary)
				}
			}
		}
	}
	return digests, key, cacheable, nil
}
