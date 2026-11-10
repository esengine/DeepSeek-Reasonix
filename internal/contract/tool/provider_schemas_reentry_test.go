package tool

import (
	"testing"
	"time"
)

type reenteringTool struct {
	surfaceTool
	reg *Registry
}

func (r reenteringTool) Description() string {
	done := make(chan struct{})
	go func() {
		r.reg.Add(surfaceTool{name: "late"})
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	r.reg.Get("alpha")
	<-done
	return "reentering"
}

func TestProviderSchemasDescriptionMayReadTheRegistryWhileAWriterWaits(t *testing.T) {
	r := NewRegistry()
	r.Add(surfaceTool{name: "alpha"})
	r.Add(reenteringTool{surfaceTool: surfaceTool{name: "beta"}, reg: r})

	finished := make(chan struct{})
	go func() {
		r.ProviderSchemas(t.Context())
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("ProviderSchemas deadlocked: Description re-read the registry while a writer was queued")
	}
}
