package runtimeservice

import (
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	"reasonix/internal/control"
	"reasonix/internal/runtimeapi"
	"reasonix/internal/skill"
)

var ErrInvalidComposerProjection = errors.New("runtime service: invalid composer projection")

// ProjectSlashArgs is the shared Local/Remote argument-completion rule. It
// delegates parsing and dynamic command semantics to control.SlashArgItems and
// only owns deterministic source ordering plus the target-neutral DTO mapping.
func ProjectSlashArgs(input string, source control.ArgData) (runtimeapi.SlashArgsResult, error) {
	if !utf8.ValidString(input) {
		return runtimeapi.SlashArgsResult{}, ErrInvalidComposerProjection
	}
	source = cloneAndSortArgData(source)
	items, from := control.SlashArgItems(input, source)
	if from < 0 || from > len(input) {
		return runtimeapi.SlashArgsResult{}, ErrInvalidComposerProjection
	}
	result := runtimeapi.SlashArgsResult{Items: []runtimeapi.SlashArgItem{}, From: from}
	for _, item := range items {
		if strings.TrimSpace(item.Label) == "" || !utf8.ValidString(item.Label) ||
			!utf8.ValidString(item.Insert) || !utf8.ValidString(item.Hint) {
			return runtimeapi.SlashArgsResult{}, ErrInvalidComposerProjection
		}
		result.Items = append(result.Items, runtimeapi.SlashArgItem{
			Label: item.Label, Insert: item.Insert, Hint: item.Hint, Descend: item.Descend,
		})
	}
	return result, nil
}

func cloneAndSortArgData(source control.ArgData) control.ArgData {
	cloneSkills := func(values []skill.Skill) []skill.Skill {
		out := append([]skill.Skill(nil), values...)
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].SlashName() != out[j].SlashName() {
				return out[i].SlashName() < out[j].SlashName()
			}
			return out[i].Scope < out[j].Scope
		})
		return out
	}
	cloneStrings := func(values []string) []string {
		out := append([]string(nil), values...)
		sort.Strings(out)
		return out
	}
	return control.ArgData{
		Skills: cloneSkills(source.Skills), DisabledSkills: cloneSkills(source.DisabledSkills),
		ServerNames: cloneStrings(source.ServerNames), ConfiguredMCP: cloneStrings(source.ConfiguredMCP),
		DisconnectedMCP: cloneStrings(source.DisconnectedMCP), ModelRefs: cloneStrings(source.ModelRefs),
		CurrentModel: source.CurrentModel, ProviderNames: cloneStrings(source.ProviderNames),
		CurrentProvider: source.CurrentProvider, PluginNames: cloneStrings(source.PluginNames),
	}
}
