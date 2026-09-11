package provider

import (
	"slices"
	"strings"
)

// OfficialDeepSeekVisionModel is the pinned vision SKU DeepSeek still routes to
// V4.1 Flash. It stays exported for pricing, effort and backfill keying.
const OfficialDeepSeekVisionModel = "deepseek-v4-flash-vision-exp"

// officialDeepSeekImageModels lists official DeepSeek IDs that accept image
// input natively. This is the single authority for image capability on the
// official endpoint: the capability resolver, the runtime vision gate and the
// local model catalog all consult it, so a new SKU is added in one place.
//
// deepseek-v4-flash and the beta alias are routed to V4.1 Flash for now; drop
// the alias once the vendor closes that transition window.
var officialDeepSeekImageModels = []string{
	"deepseek-flash",
	"deepseek-v4.1-flash-expires-on-0910",
	"deepseek-v4-flash",
	OfficialDeepSeekVisionModel,
}

// IsOfficialDeepSeekImageModel reports whether model is an official DeepSeek SKU
// with native image input. Matching is case-insensitive and trims space.
func IsOfficialDeepSeekImageModel(model string) bool {
	model = strings.TrimSpace(model)
	return slices.ContainsFunc(officialDeepSeekImageModels, func(candidate string) bool {
		return strings.EqualFold(candidate, model)
	})
}

// IsOfficialDeepSeekTextModel identifies known text-only models, not future
// SKUs. It gates the official endpoint's hard image block, so a model must stay
// listed until the vendor confirms it accepts images.
func IsOfficialDeepSeekTextModel(model string) bool {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "deepseek-v4-pro":
		return true
	}
	return false
}
