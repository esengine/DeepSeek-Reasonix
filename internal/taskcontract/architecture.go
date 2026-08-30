package taskcontract

import (
	"slices"

	"reasonix/internal/evidence"
)

const (
	architectureLargeFiles    = 8
	maxAutoIndependentReviews = 2
)

func architectureWorthy(profile evidence.EffectProfile, workspaceRoot string) bool {
	modules := map[string]struct{}{}
	files := 0
	protocol := false
	concurrency := false
	for _, target := range profile.Targets {
		if target.Path == "" {
			continue
		}
		files++
		switch evidence.ClassifyPath(target.Path, workspaceRoot) {
		case evidence.PathSchema, evidence.PathMigration, evidence.PathPublicAPI:
			protocol = true
		case evidence.PathConcurrency:
			concurrency = true
		}
		if mod := evidence.PathModule(target.Path, workspaceRoot); mod != "" {
			modules[mod] = struct{}{}
		}
	}
	if protocol || concurrency {
		return true
	}
	return files >= architectureLargeFiles || len(modules) >= 2
}

func applyArchitectureReview(class writerClass, profile evidence.EffectProfile, workspaceRoot string, reviews []ObligationKind) []ObligationKind {
	switch class {
	case writerNone, writerDocs, writerAuth:
		return reviews
	}
	if architectureWorthy(profile, workspaceRoot) {
		return ensureObligationKind(reviews, ObligationIndependentReview)
	}
	if class == writerSchema {
		return reviews
	}
	return dropObligationKind(reviews, ObligationIndependentReview)
}

func ensureObligationKind(kinds []ObligationKind, kind ObligationKind) []ObligationKind {
	if slices.Contains(kinds, kind) {
		return kinds
	}
	return append(kinds, kind)
}

func dropObligationKind(kinds []ObligationKind, kind ObligationKind) []ObligationKind {
	out := kinds[:0]
	for _, existing := range kinds {
		if existing != kind {
			out = append(out, existing)
		}
	}
	return out
}
