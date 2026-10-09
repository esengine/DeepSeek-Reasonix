package plugin

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestFailureSummaryMasksEncodedEndpointKeys(t *testing.T) {
	err := &url.Error{Op: "Post", URL: "https://host/mcp?%74oken=fixture-secret#passwd=fixture-secret", Err: errors.New("connection unavailable")}
	if got := summarizeFailureError(err); strings.Contains(got, "fixture-secret") {
		t.Fatalf("summary leaked: %s", got)
	}
}
