package openai

import (
	"net/url"
	"strings"
)

// StepFun's coding-plan surface lives under /step_plan/v1 on both official
// regional hosts. The docs hand Anthropic-SDK users a base without /v1
// (https://api.stepfun.com/step_plan) because that SDK appends /v1/messages
// itself; pasting the same base into an OpenAI-compatible client yields
// {base}/chat/completions — a 404, while the model probe's {base}/v1/models
// fallback still succeeds, so setup looks healthy and every call fails.
// Canonicalization preserves the user's regional host; only the path moves.
const (
	stepFunPlanChatPath   = "/step_plan/v1/chat/completions"
	stepFunPlanModelsPath = "/step_plan/v1/models"
)

// canonicalStepFunPlanEndpoint rewrites known official StepFun step_plan URLs
// to their complete form. Unknown or unsafe URLs stay untouched.
func canonicalStepFunPlanEndpoint(raw, canonicalPath string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if strings.ContainsAny(raw, "?#") {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Opaque != "" {
		return "", false
	}
	if !isStepFunPlanHost(u.Hostname()) {
		return "", false
	}
	if port := u.Port(); port != "" && port != "443" {
		return "", false
	}
	if u.User != nil || u.RawPath != "" {
		return "", false
	}
	if !stepFunPlanKnownPath(u.Path) {
		return "", false
	}
	return "https://" + strings.ToLower(u.Hostname()) + canonicalPath, true
}

func canonicalStepFunPlanChatURL(raw string) (string, bool) {
	return canonicalStepFunPlanEndpoint(raw, stepFunPlanChatPath)
}

// CanonicalStepFunPlanModelsURL rewrites known official StepFun step_plan URLs
// to GET /step_plan/v1/models.
func CanonicalStepFunPlanModelsURL(raw string) (string, bool) {
	return canonicalStepFunPlanEndpoint(raw, stepFunPlanModelsPath)
}

// isStepFunPlanHost matches the two official API hosts exactly. Both stepfun.com
// (China) and stepfun.ai (global) serve step_plan; relays and lookalikes stay
// untouched.
func isStepFunPlanHost(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "api.stepfun.com", "api.stepfun.ai":
		return true
	default:
		return false
	}
}

func stepFunPlanKnownPath(path string) bool {
	if path != "/" {
		path = strings.TrimSuffix(path, "/")
	}
	switch path {
	case "/step_plan", "/step_plan/v1", "/step_plan/v1/v1":
		return true
	}
	for _, leaf := range []string{"/chat/completions", "/models"} {
		for _, prefix := range []string{"/step_plan", "/step_plan/v1", "/step_plan/v1/v1"} {
			if path == prefix+leaf || path == prefix+leaf+leaf {
				return true
			}
		}
	}
	return false
}
