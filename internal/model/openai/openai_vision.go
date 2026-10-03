package openai

import (
	"strings"

	"reasonix/internal/contract/provider"
)

// resolveVision applies the official-DeepSeek model guard and normalizes the
// vision detail level. DeepSeek's official chat API takes image parts only on
// the models that declare them, and the parts need no new serializer — the
// OpenAI image_url shape is documented verbatim. The guard stays regardless:
// no capability flag may put pixels on a wire that will reject them.
func resolveVision(cfg provider.Config, officialDeepSeek, vision bool) (bool, string) {
	vision = vision && visionReachesModel(officialDeepSeek, cfg.Model)
	visionDetail, _ := cfg.Extra["vision_detail"].(string)
	visionDetail = strings.ToLower(strings.TrimSpace(visionDetail))
	if !detailAccepted(visionDetail, officialDeepSeek) {
		visionDetail = "" // auto — omit the field
	}
	return vision, visionDetail
}
