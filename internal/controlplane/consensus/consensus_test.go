package consensus

import "testing"

// #4: capping a dominant weight must bound it to maxNodeShare of the POST-cap
// total. The old code capped against the pre-cap total, so [100,1] capped to
// 0.55*101=55.55 still left the node at 55.55/56.55 ≈ 98%.
func TestCapDominantWeightsBoundsPostCapShare(t *testing.T) {
	w := capDominantWeights([]float64{100, 1})
	total := w[0] + w[1]
	if total <= 0 {
		t.Fatalf("degenerate total: %v", w)
	}
	if share := w[0] / total; share > maxNodeShare+1e-6 {
		t.Fatalf("dominant share = %.4f (weights=%v), want <= %.2f", share, w, maxNodeShare)
	}
}

func TestCapDominantWeightsHandlesMultipleLargeNodes(t *testing.T) {
	w := capDominantWeights([]float64{50, 50, 1})
	total := w[0] + w[1] + w[2]
	for i, v := range w {
		if share := v / total; share > maxNodeShare+1e-6 {
			t.Fatalf("node %d share = %.4f (weights=%v), want <= %.2f", i, share, w, maxNodeShare)
		}
	}
}

func TestCapDominantWeightsLeavesBalancedUntouched(t *testing.T) {
	in := []float64{3, 3, 4}
	w := capDominantWeights(append([]float64(nil), in...))
	for i := range in {
		if w[i] != in[i] {
			t.Fatalf("balanced weights changed: %v -> %v", in, w)
		}
	}
}
