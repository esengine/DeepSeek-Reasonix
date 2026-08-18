package responses

import (
	"net/http"
	"testing"
)

type closeTrackingTransport struct {
	closed bool
}

func (*closeTrackingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	panic("unexpected RoundTrip")
}

func (t *closeTrackingTransport) CloseIdleConnections() {
	t.closed = true
}

func TestCloseIdleConnectionsDelegatesToHTTPClient(t *testing.T) {
	transport := &closeTrackingTransport{}
	c := &client{http: &http.Client{Transport: transport}}

	c.CloseIdleConnections()
	if !transport.closed {
		t.Fatal("HTTP transport did not receive CloseIdleConnections")
	}
}

func TestCloseIdleConnectionsIsNilSafe(t *testing.T) {
	var nilClient *client
	nilClient.CloseIdleConnections()
	(&client{}).CloseIdleConnections()
}
