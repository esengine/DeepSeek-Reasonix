package catalog

import (
	"slices"
	"strings"

	"reasonix/internal/contract/config"
	"reasonix/internal/model/openai"
)

// currentMiniMaxChoices filters discovered text choices while keeping explicit
// saved IDs selectable. It does not change the persisted configuration.
func currentMiniMaxChoices(models []openai.ListedModel, entry *config.ProviderEntry) []openai.ListedModel {
	out := make([]openai.ListedModel, 0, len(models))
	var positions []int
	var current []openai.ListedModel
	for _, model := range models {
		leaf := strings.ToLower(model.ID)
		if i := strings.LastIndexByte(leaf, '/'); i >= 0 {
			leaf = leaf[i+1:]
		}
		leaf = strings.TrimSuffix(strings.TrimSuffix(leaf, ":free"), "-free")
		active := leaf == "minimax-m3.1-flash" || leaf == "minimax-m3"
		series := strings.TrimPrefix(leaf, "minimax-m")
		text := leaf == "minimax-text-01" || (series != leaf && len(series) > 0 && series[0] >= '0' && series[0] <= '9')
		if text && !active && !entry.HasModel(model.ID) {
			continue
		}
		if active {
			positions = append(positions, len(out))
			current = append(current, model)
		}
		out = append(out, model)
	}
	// Reorder only active MiniMax slots; other vendors retain their positions.
	slices.SortStableFunc(current, func(a, b openai.ListedModel) int {
		return strings.Compare(flashOrder(a.ID), flashOrder(b.ID))
	})
	for i, pos := range positions {
		out[pos] = current[i]
	}
	return out
}

func flashOrder(id string) string {
	if strings.Contains(strings.ToLower(id), "minimax-m3.1-flash") {
		return "0"
	}
	return "1"
}
