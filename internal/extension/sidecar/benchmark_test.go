package sidecar

import (
	"context"
	"slices"
	"strconv"
	"testing"
	"time"

	"reasonix/internal/extension"
	"reasonix/internal/extension/dispatch"
	"reasonix/internal/pluginpkg"
)

// BenchmarkExtensionSidecarStartup measures a real no-op sidecar process
// spawn plus Extension Protocol handshake. Shutdown is kept outside the
// benchmark timer; sampled p50/p95 cover StartClient only.
func BenchmarkExtensionSidecarStartup(b *testing.B) {
	pkg, installed := fakeSidecarPackage(b, "benchmark", nil)
	opts := ClientOptions{Package: pkg, Installed: installed, Session: testSessionContext()}
	b.ReportAllocs()
	const maxSamples = 100_000
	samples := make([]int64, 0, maxSamples)
	for b.Loop() {
		start := time.Now()
		client, err := StartClient(context.Background(), opts)
		if err != nil {
			b.Fatal(err)
		}
		elapsed := time.Since(start).Nanoseconds()
		b.StopTimer()
		if err := client.Close(); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if len(samples) < maxSamples {
			samples = append(samples, elapsed)
		}
	}
	b.StopTimer()
	reportSidecarPercentiles(b, samples)
}

// BenchmarkExtensionSidecarDispatchLatency measures actual NDJSON RPC through
// one and four real no-op sidecar processes on turn and tool hot paths. Calls
// are serial by protocol contract, so the four-sidecar case exposes additive
// latency instead of only the dispatcher's in-process overhead.
func BenchmarkExtensionSidecarDispatchLatency(b *testing.B) {
	for _, count := range []int{1, 4} {
		b.Run("Turn/NoopSidecars"+strconv.Itoa(count), func(b *testing.B) {
			d := benchmarkSidecarDispatcher(b, extension.PointInputReceive, count)
			payload := dispatch.InputPayload{Text: "hello"}
			benchmarkSidecarLatency(b, func() error {
				_, err := d.Intercept(context.Background(), extension.PointInputReceive, &payload)
				return err
			})
		})
		b.Run("Tool/NoopSidecars"+strconv.Itoa(count), func(b *testing.B) {
			d := benchmarkSidecarDispatcher(b, extension.PointToolBefore, count)
			payload := dispatch.ToolBeforePayload{Name: "bash", Arguments: `{"cmd":"pwd"}`}
			benchmarkSidecarLatency(b, func() error {
				_, err := d.Intercept(context.Background(), extension.PointToolBefore, &payload)
				return err
			})
		})
	}
}

func benchmarkSidecarDispatcher(b *testing.B, point extension.InterceptorPoint, count int) *dispatch.Dispatcher {
	b.Helper()
	chain := make([]extension.Contribution, 0, count)
	clients := make(map[string]*Client, count)
	for i := range count {
		pluginID := "benchmark-" + strconv.Itoa(i)
		client := startFakeClient(b, func(rt *pluginpkg.RuntimeSpec) {
			rt.Intercepts = []string{string(point)}
		}, nil)
		clients[pluginID] = client
		chain = append(chain, extension.Contribution{
			Kind: extension.KindInterceptor,
			ID:   string(point),
			Source: extension.ContributionSource{
				Scope:    extension.ScopePlugin,
				PluginID: pluginID,
			},
		})
	}
	return dispatch.New(map[extension.InterceptorPoint][]extension.Contribution{point: chain}, nil,
		func(pluginID string) dispatch.Client { return clients[pluginID] }, nil, dispatch.Options{})
}

func benchmarkSidecarLatency(b *testing.B, fn func() error) {
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
	reportSidecarPercentiles(b, samples)
}

func reportSidecarPercentiles(b *testing.B, samples []int64) {
	b.Helper()
	slices.Sort(samples)
	if len(samples) == 0 {
		return
	}
	b.ReportMetric(float64(samples[(len(samples)-1)*50/100]), "p50-ns/op")
	b.ReportMetric(float64(samples[(len(samples)-1)*95/100]), "p95-ns/op")
}

var _ dispatch.Client = (*Client)(nil)
