package agent

import (
	"strings"
	"sync"
)

// WarmupPredictor predicts which tools will likely be needed based on query
// patterns and pre-warms their schemas to reduce latency.
// OPT-61: Tool schema warmup prediction system.
type WarmupPredictor struct {
	mu                 sync.RWMutex
	predictions        map[string][]string
	totalPredictions   int
	correctPredictions int
	warmedTools        int
	lastPredicted      []string
}

// WarmupPredictorStats holds statistics about warmup prediction accuracy.
type WarmupPredictorStats struct {
	TotalPredictions   int
	CorrectPredictions int
	AccuracyRate       float64
	WarmedTools        int
}

// NewWarmupPredictor creates a new WarmupPredictor with default pattern-to-tool
// mappings.
func NewWarmupPredictor() *WarmupPredictor {
	return &WarmupPredictor{
		predictions: map[string][]string{
			"file":   {"read_file", "edit_file", "grep", "glob"},
			"search": {"web_search", "web_fetch"},
			"run":    {"bash"},
			"plan":   {"todo_write"},
		},
	}
}

// PredictTools examines the query and returns a list of tools that are likely
// to be needed based on pattern matching.
func (w *WarmupPredictor) PredictTools(query string) []string {
	w.mu.Lock()
	defer w.mu.Unlock()

	queryLower := strings.ToLower(query)
	var predicted []string

	for pattern, tools := range w.predictions {
		if strings.Contains(queryLower, pattern) {
			predicted = append(predicted, tools...)
		}
	}

	w.lastPredicted = predicted
	w.warmedTools += len(predicted)

	return predicted
}

// RecordPrediction records the actual tools that were used for a query, allowing
// the predictor to track accuracy of its predictions.
func (w *WarmupPredictor) RecordPrediction(query string, actualTools []string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.totalPredictions++

	actualSet := make(map[string]bool, len(actualTools))
	for _, t := range actualTools {
		actualSet[t] = true
	}

	for _, predicted := range w.lastPredicted {
		if actualSet[predicted] {
			w.correctPredictions++
			return
		}
	}
}

// ShouldWarmup returns true if the given tool was predicted in the last
// PredictTools call, indicating its schema should be pre-warmed.
func (w *WarmupPredictor) ShouldWarmup(toolName string) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()

	for _, t := range w.lastPredicted {
		if t == toolName {
			return true
		}
	}
	return false
}

// GetStats returns current statistics about warmup prediction performance.
func (w *WarmupPredictor) GetStats() WarmupPredictorStats {
	w.mu.RLock()
	defer w.mu.RUnlock()

	accuracyRate := 0.0
	if w.totalPredictions > 0 {
		accuracyRate = float64(w.correctPredictions) / float64(w.totalPredictions)
	}

	return WarmupPredictorStats{
		TotalPredictions:   w.totalPredictions,
		CorrectPredictions: w.correctPredictions,
		AccuracyRate:       accuracyRate,
		WarmedTools:        w.warmedTools,
	}
}

// Reset clears all prediction statistics and state.
func (w *WarmupPredictor) Reset() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.totalPredictions = 0
	w.correctPredictions = 0
	w.warmedTools = 0
	w.lastPredicted = nil
}
