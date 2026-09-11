// Package replay computes paired-run medians for use_capability eval harnesses.
package replay

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// Pair is one proxy-vs-baseline observation.
type Pair struct {
	Name              string  `json:"name"`
	ProxyListCount    int     `json:"proxy_list_count"`
	BaselineListCount int     `json:"baseline_list_count"`
	ProxyLatencyMs    float64 `json:"proxy_latency_ms"`
	BaselineLatencyMs float64 `json:"baseline_latency_ms"`
}

// Report is the median of five (or more) paired runs.
type Report struct {
	Pairs              int     `json:"pairs"`
	MedianListDelta    float64 `json:"median_list_delta"`
	MedianLatencyDelta float64 `json:"median_latency_delta"`
}

// LoadPairs reads a JSON array of paired runs.
func LoadPairs(path string) ([]Pair, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var pairs []Pair
	if err := json.Unmarshal(raw, &pairs); err != nil {
		return nil, fmt.Errorf("decode paired runs: %w", err)
	}
	return pairs, nil
}

// MedianReport returns median(proxy-baseline) for list count and latency.
func MedianReport(pairs []Pair) Report {
	list := make([]float64, 0, len(pairs))
	lat := make([]float64, 0, len(pairs))
	for _, p := range pairs {
		list = append(list, float64(p.ProxyListCount-p.BaselineListCount))
		lat = append(lat, p.ProxyLatencyMs-p.BaselineLatencyMs)
	}
	return Report{
		Pairs:              len(pairs),
		MedianListDelta:    median(list),
		MedianLatencyDelta: median(lat),
	}
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}
