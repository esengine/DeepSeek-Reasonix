package provider

import (
	"encoding/json"
	"net/url"
)

const SourcesAvailable = "available"
const SourcesNotProvided = "not_provided"

func HasUsableSearchSources(hits []ServerSearchHit) bool {
	for _, hit := range hits {
		u, err := url.Parse(hit.URL)
		if err == nil && u.Host != "" && u.User == nil && (u.Scheme == "https" || u.Scheme == "http") {
			return true
		}
	}
	return false
}

func ServerSearchSourcesStatus(call ServerSearchCall) string {
	if HasUsableSearchSources(call.Results) {
		return SourcesAvailable
	}
	var raw any
	if json.Unmarshal(call.Raw, &raw) != nil {
		return ""
	}
	switch value := raw.(type) {
	case []any:
		return SourcesNotProvided
	case map[string]any:
		if value["type"] == "web_search_call" && value["status"] == "completed" {
			return SourcesNotProvided
		}
	}
	return ""
}

// ServerSearchDisplayOutput is presentation data, never a raw replay item.
func ServerSearchDisplayOutput(call ServerSearchCall) string {
	if call.SourcesStatus == SourcesNotProvided {
		return `{"sources":[],"sources_status":"not_provided"}`
	}
	return FormatServerSearchOutput(call.Results)
}
