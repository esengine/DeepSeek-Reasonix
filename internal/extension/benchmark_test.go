package extension

import (
	"context"
	"slices"
	"testing"
	"time"

	"reasonix/internal/extensioncontract"
)

// BenchmarkExtensionKernelStartup measures the immutable snapshot assembly
// portion of startup with no extensions and with a representative 64-entry
// interceptor catalog. Process spawn and sidecar handshake latency are
// intentionally excluded and should be measured by extension authors.
func BenchmarkExtensionKernelStartup(b *testing.B) {
	b.Run("NoExtensions", func(b *testing.B) {
		benchmarkBuildLatency(b, NewBuilder().WithSystemPrompt("stable prompt"))
	})

	contributions := make([]Contribution, 0, 64)
	for i := range 64 {
		contributions = append(contributions, Contribution{
			Kind: KindInterceptor,
			ID:   string(PointToolBefore),
			Source: ContributionSource{
				Scope:    ScopePlugin,
				PluginID: "plugin-" + benchmarkIndex(i),
			},
			Priority: i%21 - 10,
		})
	}
	builder := NewBuilder().WithSystemPrompt("stable prompt").AddContributor(ContributorFunc{
		ContributorName: "benchmark",
		Fn: func(context.Context) ([]Contribution, error) {
			return contributions, nil
		},
	})
	b.Run("64Interceptors", func(b *testing.B) { benchmarkBuildLatency(b, builder) })
}

func benchmarkIndex(i int) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[(i>>4)&15], digits[i&15]})
}

func benchmarkBuildLatency(b *testing.B, builder *Builder) {
	b.Helper()
	b.ReportAllocs()
	const maxSamples = 100_000
	samples := make([]int64, 0, maxSamples)
	for b.Loop() {
		start := time.Now()
		_, runtimeSet, err := builder.Build(context.Background())
		if err != nil {
			b.Fatal(err)
		}
		if err := runtimeSet.Close(); err != nil {
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

// BenchmarkDependencyGraphAndPlan measures graph resolution and no-op / full
// plan diffs as the component count grows (performance baseline for rebuild).
func BenchmarkDependencyGraphAndPlan(b *testing.B) {
	for _, n := range []int{8, 64, 256} {
		comps := make([]ComponentDescriptor, 0, n)
		for i := range n {
			id := ComponentID("plugin/" + benchmarkIndex(i%256) + benchmarkIndex(i/256))
			comps = append(comps, ComponentDescriptor{
				ID: id,
				Provides: []extensioncontract.Capability{{
					Key: extensioncontract.CapabilityKey{
						Namespace: string(id), Kind: "interceptors", ID: "default",
					},
					Version: "1.0.0",
				}},
			})
		}
		b.Run("graph/"+itoa(n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := BuildDependencyGraph(comps); err != nil {
					b.Fatal(err)
				}
			}
		})
		g, err := BuildDependencyGraph(comps)
		if err != nil {
			b.Fatal(err)
		}
		b.Run("plan-noop/"+itoa(n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = DiffRuntimePlan(g, g, 1, 2)
			}
		})
		// Full reload of every component identity (version bump).
		reloaded := make([]ComponentDescriptor, len(comps))
		copy(reloaded, comps)
		for i := range reloaded {
			if len(reloaded[i].Provides) > 0 {
				reloaded[i].Provides[0].Version = "2.0.0"
			}
		}
		g2, err := BuildDependencyGraph(reloaded)
		if err != nil {
			b.Fatal(err)
		}
		b.Run("plan-full/"+itoa(n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = DiffRuntimePlan(g, g2, 1, 2)
			}
		})
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [16]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
