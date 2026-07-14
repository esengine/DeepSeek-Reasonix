package agent

import (
	"sync"
)

// EfficiencyScore represents a token efficiency score with a detailed
// breakdown of the contributing factors and actionable recommendations.
type EfficiencyScore struct {
	Score            float64
	Grade            string
	CacheEfficiency  float64
	OutputEfficiency float64
	ToolEfficiency   float64
	Recommendations  []string
}

// EfficiencyStats holds overall statistics about efficiency scoring.
type EfficiencyStats struct {
	TotalScored   int
	AvgEfficiency float64
	BestScore     float64
	WorstScore    float64
}

// TokenEfficiencyScorer scores overall token efficiency and provides
// recommendations for improvement. The score is a weighted combination of:
//   - Cache hit ratio (40% weight)
//   - Output/input token ratio (30% weight)
//   - Tool call efficiency (30% weight)
type TokenEfficiencyScorer struct {
	mu              sync.RWMutex
	totalScored     int
	avgEfficiency   float64
	bestScore       float64
	worstScore      float64
	recommendations []string
}

// NewTokenEfficiencyScorer creates a new TokenEfficiencyScorer.
func NewTokenEfficiencyScorer() *TokenEfficiencyScorer {
	return &TokenEfficiencyScorer{}
}

// ScoreEfficiency calculates an efficiency score (0-100) based on the given
// token metrics and returns a detailed EfficiencyScore with grade and
// recommendations.
//
// The score is computed as:
//   - cacheEfficiency * 0.4 (cache hit ratio)
//   - outputEfficiency * 0.3 (output/input ratio optimality)
//   - toolEfficiency  * 0.3 (tokens per tool call)
func (s *TokenEfficiencyScorer) ScoreEfficiency(
	inputTokens int,
	outputTokens int,
	cacheHitTokens int,
	cacheMissTokens int,
	toolCalls int,
) EfficiencyScore {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Calculate individual efficiency components (each 0-100)
	cacheEff := tesCalcCacheEfficiency(cacheHitTokens, cacheMissTokens)
	outputEff := tesCalcOutputEfficiency(inputTokens, outputTokens)
	toolEff := tesCalcToolEfficiency(toolCalls, inputTokens, outputTokens)

	// Overall weighted score
	score := cacheEff*0.4 + outputEff*0.3 + toolEff*0.3

	grade := tesScoreToGrade(score)
	recs := tesGenerateRecommendations(cacheEff, outputEff, toolEff)

	// Update running statistics
	s.totalScored++
	s.updateStats(score)

	// Store latest recommendations
	s.recommendations = recs

	return EfficiencyScore{
		Score:            score,
		Grade:            grade,
		CacheEfficiency:  cacheEff,
		OutputEfficiency: outputEff,
		ToolEfficiency:   toolEff,
		Recommendations:  recs,
	}
}

// GetOverallStats returns overall statistics about efficiency scoring.
func (s *TokenEfficiencyScorer) GetOverallStats() EfficiencyStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return EfficiencyStats{
		TotalScored:   s.totalScored,
		AvgEfficiency: s.avgEfficiency,
		BestScore:     s.bestScore,
		WorstScore:    s.worstScore,
	}
}

// GetRecommendations returns the current set of recommendations generated
// from the most recent ScoreEfficiency call.
func (s *TokenEfficiencyScorer) GetRecommendations() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	recs := make([]string, len(s.recommendations))
	copy(recs, s.recommendations)
	return recs
}

// Reset resets all statistics and state.
func (s *TokenEfficiencyScorer) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.totalScored = 0
	s.avgEfficiency = 0
	s.bestScore = 0
	s.worstScore = 0
	s.recommendations = nil
}

// updateStats updates the running average and best/worst scores.
// Caller must hold the write lock.
func (s *TokenEfficiencyScorer) updateStats(score float64) {
	// Running average: (old_avg * (n-1) + new_score) / n
	s.avgEfficiency = (s.avgEfficiency*float64(s.totalScored-1) + score) / float64(s.totalScored)

	if s.totalScored == 1 {
		// First score initializes best and worst
		s.bestScore = score
		s.worstScore = score
	} else {
		if score > s.bestScore {
			s.bestScore = score
		}
		if score < s.worstScore {
			s.worstScore = score
		}
	}
}

// ---------------------------------------------------------------------------
// Internal helpers (prefixed with tes to avoid naming conflicts)
// ---------------------------------------------------------------------------

// tesCalcCacheEfficiency calculates cache efficiency as a 0-100 score based
// on the ratio of cache hit tokens to total cache tokens.
func tesCalcCacheEfficiency(cacheHitTokens, cacheMissTokens int) float64 {
	total := cacheHitTokens + cacheMissTokens
	if total == 0 {
		return 100 // No cache usage needed, perfect efficiency
	}
	return float64(cacheHitTokens) / float64(total) * 100
}

// tesCalcOutputEfficiency calculates output efficiency as a 0-100 score.
// The optimal output/input ratio is 0.1-0.5 (meaningful but not verbose).
// Scores decrease for ratios that are too low (wasted input) or too high
// (excessively verbose output).
func tesCalcOutputEfficiency(inputTokens, outputTokens int) float64 {
	if inputTokens == 0 {
		return 100
	}

	ratio := float64(outputTokens) / float64(inputTokens)

	switch {
	case ratio <= 0:
		return 0
	case ratio <= 0.1:
		return ratio * 100 // Scales from 0 to ~10
	case ratio <= 0.5:
		return 100 // Optimal range
	case ratio <= 1.0:
		return 100 - (ratio-0.5)*100 // Decreases from 100 to 50
	default:
		// ratio > 1.0: output exceeds input, increasingly inefficient
		if ratio >= 2.0 {
			return 0
		}
		return 50 - (ratio-1.0)*50 // Decreases from 50 to 0
	}
}

// tesCalcToolEfficiency calculates tool call efficiency as a 0-100 score.
// Higher tokens-per-call indicates each tool call contributes meaningfully.
// Few tokens per call suggests redundant or unnecessary calls.
func tesCalcToolEfficiency(toolCalls, inputTokens, outputTokens int) float64 {
	totalTokens := inputTokens + outputTokens

	if toolCalls == 0 {
		return 100 // No tool calls needed, perfect efficiency
	}

	if totalTokens == 0 {
		return 0
	}

	tokensPerCall := float64(totalTokens) / float64(toolCalls)

	switch {
	case tokensPerCall >= 500:
		return 100
	case tokensPerCall >= 100:
		// Scale from 50 to 100
		return 50 + (tokensPerCall-100)/400*50
	default:
		// Scale from 0 to 50
		return tokensPerCall / 100 * 50
	}
}

// tesScoreToGrade converts a numeric score (0-100) to a letter grade.
func tesScoreToGrade(score float64) string {
	switch {
	case score >= 90:
		return "A"
	case score >= 80:
		return "B"
	case score >= 70:
		return "C"
	case score >= 60:
		return "D"
	default:
		return "F"
	}
}

// tesGenerateRecommendations generates actionable recommendations based on
// the individual efficiency component scores.
func tesGenerateRecommendations(cacheEff, outputEff, toolEff float64) []string {
	var recs []string

	if cacheEff < 30 {
		recs = append(recs, "Enable cache prefix stabilization to improve cache reuse")
	} else if cacheEff < 50 {
		recs = append(recs, "Improve cache hit ratio by stabilizing system prompts and tool schemas")
	}

	if outputEff < 30 {
		recs = append(recs, "Review prompt structure to improve output efficiency")
	} else if outputEff < 50 {
		recs = append(recs, "Optimize output/input token ratio - output may be too verbose or too sparse")
	}

	if toolEff < 30 {
		recs = append(recs, "Batch tool calls to improve per-call token efficiency")
	} else if toolEff < 50 {
		recs = append(recs, "Reduce redundant tool calls by implementing call deduplication")
	}

	if cacheEff >= 80 && outputEff >= 80 && toolEff >= 80 {
		recs = append(recs, "Excellent efficiency - maintain current optimization strategies")
	}

	if len(recs) == 0 {
		recs = append(recs, "Efficiency is good - continue monitoring for areas of improvement")
	}

	return recs
}
