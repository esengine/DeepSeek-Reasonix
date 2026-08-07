package extension

import (
	"context"
	"slices"
	"testing"
	"time"
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
