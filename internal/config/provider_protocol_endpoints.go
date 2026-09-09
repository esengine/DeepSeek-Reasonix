package config

import (
	_ "embed"
	"encoding/json"
	"maps"
)

// ProviderProtocolEndpoint is a documented SDK base URL, not a complete request URL.
// This is catalog metadata; it does not migrate saved user connections.
type ProviderProtocolEndpoint struct {
	BaseURL       string `json:"baseUrl"`
	Source        string `json:"source"`
	CheckedOn     string `json:"checkedOn"`
	AuthHeader    bool   `json:"authHeader,omitempty"`
	ResponsesMode string `json:"responsesMode,omitempty"`
}

//go:embed provider_protocol_endpoints.json
var protocolEndpointJSON []byte

var documentedProtocolEndpoints = func() map[string]map[string]ProviderProtocolEndpoint {
	var result map[string]map[string]ProviderProtocolEndpoint
	if err := json.Unmarshal(protocolEndpointJSON, &result); err != nil {
		panic(err)
	}
	return result
}()

func ProtocolEndpointsForCatalog(c ProviderCatalog) map[string]ProviderProtocolEndpoint {
	source := documentedProtocolEndpoints[c.BrandID+"|"+c.Region+"|"+c.Product]
	result := make(map[string]ProviderProtocolEndpoint, len(source))
	maps.Copy(result, source)
	return result
}
