package dispatch

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"testing"
	"time"

	"reasonix/internal/extension"
	"reasonix/internal/extension/protocol"
)

type benchmarkClient struct{}

func (benchmarkClient) Intercept(context.Context, protocol.InterceptEvent, json.RawMessage, time.Duration) (protocol.InterceptResult, error) {
	return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
}

func (benchmarkClient) TryNotifyEvent(protocol.InterceptEvent, json.RawMessage) error { return nil }

// BenchmarkDispatchLatency captures both Go's aggregate ns/op and sampled
// p50/p95 latency for the host guard, one no-op sidecar, and four serial
// no-op sidecars on turn and tool hot paths. Real sidecar latency is additive
// on top of these host-only numbers.
func BenchmarkDispatchLatency(b *testing.B) {
	b.Run("Turn/NoExtensionsHostGuard", func(b *testing.B) {
		var d *Dispatcher
		payload := InputPayload{Text: "hello"}
		benchmarkLatency(b, func() error {
			if d != nil {
				_, err := d.Intercept(context.Background(), extension.PointInputReceive, &payload)
				return err
			}
			return nil
		})
	})
	for _, count := range []int{1, 4} {
		b.Run("Turn/NoopInterceptors"+strconv.Itoa(count), func(b *testing.B) {
			d := benchmarkDispatcher(extension.PointInputReceive, count)
			payload := InputPayload{Text: "hello"}
			benchmarkLatency(b, func() error {
				_, err := d.Intercept(context.Background(), extension.PointInputReceive, &payload)
				return err
			})
		})
		b.Run("Tool/NoopInterceptors"+strconv.Itoa(count), func(b *testing.B) {
			d := benchmarkDispatcher(extension.PointToolBefore, count)
			payload := ToolBeforePayload{Name: "bash", Arguments: `{"cmd":"pwd"}`}
			benchmarkLatency(b, func() error {
				_, err := d.Intercept(context.Background(), extension.PointToolBefore, &payload)
				return err
			})
		})
	}
}

func benchmarkDispatcher(point extension.InterceptorPoint, count int) *Dispatcher {
	chain := make([]extension.Contribution, 0, count)
	clients := make(map[string]Client, count)
	for i := range count {
		pluginID := string(rune('a' + i))
		chain = append(chain, extension.Contribution{
			Kind: extension.KindInterceptor, ID: string(point),
			Source: extension.ContributionSource{Scope: extension.ScopePlugin, PluginID: pluginID},
		})
		clients[pluginID] = benchmarkClient{}
	}
	return New(map[extension.InterceptorPoint][]extension.Contribution{point: chain}, nil,
		func(pluginID string) Client { return clients[pluginID] }, nil, Options{})
}

func benchmarkLatency(b *testing.B, fn func() error) {
	b.Helper()
	b.ReportAllocs()
	const maxSamples = 100_000
	samples := make([]int64, 0, maxSamples)
	for b.Loop() {
		start := time.Now()
		if err := fn(); err != nil {
			b.Fatal(err)
		}
		if len(samples) < maxSamples {
			samples = append(samples, time.Since(start).Nanoseconds())
		}
	}
	b.StopTimer()
	slices.Sort(samples)
	if len(samples) == 0 {
		return
	}
	b.ReportMetric(float64(samples[(len(samples)-1)*50/100]), "p50-ns/op")
	b.ReportMetric(float64(samples[(len(samples)-1)*95/100]), "p95-ns/op")
}
