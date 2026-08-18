package agent

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
)

type identifiedGate struct {
	id int
}

func (*identifiedGate) Check(context.Context, string, json.RawMessage, bool) (bool, string, error) {
	return true, "", nil
}

func TestAgentServicesGateSwapIsRaceSafe(t *testing.T) {
	first := &identifiedGate{id: 1}
	second := &identifiedGate{id: 2}
	services := agentServices{gate: first}

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(9)
	go func() {
		defer wg.Done()
		<-start
		for i := range 10_000 {
			if i%2 == 0 {
				services.setGate(second)
			} else {
				services.setGate(first)
			}
		}
	}()
	for range 8 {
		go func() {
			defer wg.Done()
			<-start
			for range 10_000 {
				got := services.gateSnapshot()
				if got != first && got != second {
					t.Errorf("gate snapshot = %#v, want one complete installed gate", got)
					return
				}
			}
		}()
	}
	close(start)
	wg.Wait()
}
