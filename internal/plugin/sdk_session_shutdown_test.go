package plugin

import (
	"context"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSDKSessionRetiresEndpointBeforeCancellingProcessContext(t *testing.T) {
	for _, shutdown := range []string{"close", "invalidate"} {
		t.Run(shutdown, func(t *testing.T) {
			transport := newInMemorySDKTransport(t, func() *mcpsdk.Server {
				return mcpsdk.NewServer(&mcpsdk.Implementation{Name: "shutdown", Version: "1"}, nil)
			})
			factory := transport.endpointFactory
			retired := make(chan error, 1)
			transport.endpointFactory = func(ctx context.Context) (sdkEndpoint, error) {
				endpoint, err := factory(ctx)
				endpoint.close = func() { retired <- ctx.Err() }
				return endpoint, err
			}
			managed, err := transport.acquire(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if shutdown == "close" {
				transport.close()
			} else {
				transport.invalidate(managed)
			}
			if err := <-retired; err != nil {
				t.Fatalf("process context cancelled before endpoint could perform graceful retirement: %v", err)
			}
		})
	}
}
