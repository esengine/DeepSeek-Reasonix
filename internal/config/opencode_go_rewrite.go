package config

import (
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// rawTOMLSet edits only the owning assignment. Unrelated text, including unknown
// keys and comments, is retained. Tables and quoted/dotted keys are supported.
func rawTOMLSet(body string, path []string, value any) (string, error) {
	lines := strings.Split(body, "\n")
	var section []string
	insert := 0
	parentSeen := len(path) == 1
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if header := tomlSectionHeader(line); header != "" {
			section = rawTOMLKeyPath(strings.Trim(header, "[]"))
			if reflect.DeepEqual(section, path[:len(path)-1]) {
				insert, parentSeen = i+1, true
			}
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq, err := findTOMLAssignmentEquals(lines[i], 0, len(lines[i]))
		if err != nil || eq < 0 {
			continue
		}
		key := rawTOMLKeyPath(strings.TrimSpace(lines[i][:eq]))
		full := append(append([]string{}, section...), key...)
		if len(section) == 0 && len(path) > 1 && path[0] == "providers" {
			full = append([]string{"providers"}, full...)
		}
		// Consume an entire multiline value, so embedded fake headers/keys
		// cannot be mistaken for configuration syntax.
		end := i
		var decoded map[string]any
		for ; end < len(lines); end++ {
			if _, e := toml.Decode("v = "+strings.Join(append([]string{lines[i][eq+1:]}, lines[i+1:end+1]...), "\n"), &decoded); e == nil {
				break
			}
		}
		if end == len(lines) {
			return body, fmt.Errorf("cannot locate TOML value for %s", strings.Join(full, "."))
		}
		if len(full) <= len(path) && reflect.DeepEqual(full, path[:len(full)]) {
			v := value
			if len(full) < len(path) {
				m, ok := decoded["v"].(map[string]any)
				if !ok {
					i = end
					continue
				}
				root := m
				for _, k := range path[len(full) : len(path)-1] {
					nested, ok := m[k].(map[string]any)
					if !ok {
						nested = map[string]any{}
						m[k] = nested
					}
					m = nested
				}
				m[path[len(path)-1]] = value
				v = root
			}
			encoded, err := rawTOMLValue(v)
			if err != nil {
				return body, err
			}
			replacement := replaceTOMLScalarAssignment(lines[i], encoded)
			// Retain comments inside an edited multiline array as adjacent
			// comments; the rest of the document remains byte-for-byte intact.
			var comments []string
			for n := i + 1; n <= end; n++ {
				if at := tomlInlineCommentIndex(lines[n]); at >= 0 {
					comments = append(comments, lines[n][at:])
				}
			}
			out := append([]string{}, lines[:i]...)
			out = append(out, comments...)
			out = append(out, replacement)
			out = append(out, lines[end+1:]...)
			return strings.Join(out, "\n"), nil
		}
		i = end
	}
	encoded, err := rawTOMLValue(value)
	if err != nil {
		return body, err
	}
	assignment := strconv.Quote(path[len(path)-1]) + " = " + encoded
	if parentSeen {
		lines = append(lines[:insert], append([]string{assignment}, lines[insert:]...)...)
		return strings.Join(lines, "\n"), nil
	}
	keys := make([]string, len(path)-1)
	for i, k := range path[:len(path)-1] {
		keys[i] = strconv.Quote(k)
	}
	return strings.TrimRight(body, "\n") + "\n[" + strings.Join(keys, ".") + "]\n" + assignment + "\n", nil
}

func rawTOMLKeyPath(s string) []string {
	// Let the validated TOML parser handle escapes and quoted dots.
	var m map[string]any
	if _, err := toml.Decode(s+" = 0", &m); err != nil {
		return nil
	}
	var out []string
	for len(m) == 1 {
		for k, v := range m {
			out = append(out, k)
			m, _ = v.(map[string]any)
		}
	}
	return out
}

func rawTOMLValue(v any) (string, error) {
	if v == nil {
		return "", fmt.Errorf("cannot encode absent TOML value")
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		return rawTOMLValue(rv.Elem().Interface())
	}
	switch rv.Kind() {
	case reflect.String:
		return strconv.Quote(rv.String()), nil
	case reflect.Bool:
		return strconv.FormatBool(rv.Bool()), nil
	case reflect.Int, reflect.Int64, reflect.Int32:
		return strconv.FormatInt(rv.Int(), 10), nil
	case reflect.Float64, reflect.Float32:
		return strconv.FormatFloat(rv.Float(), 'g', -1, 64), nil
	case reflect.Slice, reflect.Array:
		var parts []string
		for i := range rv.Len() {
			s, err := rawTOMLValue(rv.Index(i).Interface())
			if err != nil {
				return "", err
			}
			parts = append(parts, s)
		}
		return "[" + strings.Join(parts, ", ") + "]", nil
	case reflect.Map:
		var keys []string
		for _, k := range rv.MapKeys() {
			keys = append(keys, k.String())
		}
		sort.Strings(keys)
		var parts []string
		for _, k := range keys {
			s, err := rawTOMLValue(rv.MapIndex(reflect.ValueOf(k)).Interface())
			if err != nil {
				return "", err
			}
			parts = append(parts, strconv.Quote(k)+" = "+s)
		}
		return "{ " + strings.Join(parts, ", ") + " }", nil
	}
	return "", fmt.Errorf("unsupported TOML edit value %T", v)
}

func rewriteOpenCodeGoConfig(body string, before, after *Config, additions []openCodeGoGroup) (string, error) {
	if reflect.DeepEqual(before.Providers, after.Providers) && reflect.DeepEqual(before.Agent, after.Agent) && reflect.DeepEqual(before.Bot, after.Bot) && reflect.DeepEqual(before.Desktop, after.Desktop) && before.DefaultModel == after.DefaultModel && len(additions) == 0 {
		return rawTOMLSet(body, []string{"config_version"}, openCodeGoUpgradeVersion)
	}
	lines := strings.Split(body, "\n")
	blocks := providerTOMLBlocks(lines)
	if len(blocks) != len(before.Providers) {
		expanded, err := expandOpenCodeGoInlineProviders(body)
		if err != nil {
			return body, err
		}
		return rewriteOpenCodeGoConfig(expanded, before, after, additions)
	}
	// Extend each provider span through its nested tables only.
	for i := range blocks {
		for blocks[i].end < len(lines) {
			h := tomlSectionHeader(lines[blocks[i].end])
			p := rawTOMLKeyPath(strings.Trim(h, "[]"))
			if h != "" && (len(p) < 2 || p[0] != "providers") {
				break
			}
			blocks[i].end++
		}
	}
	var originals []string
	for _, b := range blocks {
		originals = append(originals, strings.Join(lines[b.start+1:b.end], "\n"))
	}
	// after.Providers holds each split group right behind its source, so the
	// original index i maps to i plus the groups split from earlier sources.
	afterIndex := make([]int, len(blocks))
	for i := range blocks {
		if i > 0 {
			afterIndex[i] = afterIndex[i-1] + 1
			for _, addition := range additions {
				if addition.source == i-1 {
					afterIndex[i]++
				}
			}
		}
	}
	for i := range slices.Backward(blocks) {
		next, err := patchOpenCodeGoProvider(originals[i], before.Providers[i], after.Providers[afterIndex[i]])
		if err != nil {
			return body, err
		}
		var siblings []string
		for _, addition := range additions {
			if addition.source != i {
				continue
			}
			raw, err := patchOpenCodeGoProvider(originals[i], before.Providers[i], addition.entry)
			if err != nil {
				return body, err
			}
			siblings = append(siblings, "", "[[providers]]")
			siblings = append(siblings, strings.Split(strings.TrimRight(raw, "\n"), "\n")...)
		}
		b := blocks[i]
		replacement := strings.Split(next, "\n")
		if len(siblings) > 0 {
			replacement = append(append(trimTrailingBlankLines(replacement), siblings...), "")
		}
		lines = append(lines[:b.start+1], append(replacement, lines[b.end:]...)...)
	}
	body = strings.Join(lines, "\n")
	return rewriteOpenCodeGoReferences(body, before, after)
}

func trimTrailingBlankLines(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// verifyOpenCodeGoRewrite proves the lexical edit still describes the planned
// configuration before any byte reaches disk. Every rewrite path ends here.
func verifyOpenCodeGoRewrite(body string, after *Config) error {
	var check Config
	if _, err := toml.Decode(body, &check); err != nil {
		return fmt.Errorf("migration readback: %w", err)
	}
	if check.ConfigVersion != openCodeGoUpgradeVersion {
		return fmt.Errorf("migration readback: config_version %d", check.ConfigVersion)
	}
	if check.DefaultModel != after.DefaultModel {
		return fmt.Errorf("migration readback: default_model %q", check.DefaultModel)
	}
	if len(check.Providers) != len(after.Providers) {
		return fmt.Errorf("provider count changed during lexical rewrite")
	}
	for i, p := range check.Providers {
		want := after.Providers[i]
		if p.Name != want.Name || p.Kind != want.Kind || p.BaseURL != want.BaseURL || !reflect.DeepEqual(p.ModelList(), want.ModelList()) {
			return fmt.Errorf("provider %q failed migration readback", want.Name)
		}
	}
	return nil
}

// Expand the uncommon inline-array form lexically. Only structural separators
// change; string contents, unknown fields and comments remain in the document.
func expandOpenCodeGoInlineProviders(body string) (string, error) {
	start, end, err := providerTOMLInlineArrayRange(body)
	if err != nil {
		return body, err
	}
	blocks, err := providerTOMLInlineBlocks(body)
	if err != nil {
		return body, fmt.Errorf("cannot safely map inline providers: %w", err)
	}
	if len(blocks) == 0 {
		return body, fmt.Errorf("cannot safely map inline providers: no provider blocks")
	}
	assignment := strings.LastIndex(body[:start], "\n") + 1
	var tables strings.Builder
	outside := body[start+1 : end]
	for _, b := range slices.Backward(blocks) {
		a, z := b.start-start-1, b.end-start
		outside = outside[:a] + outside[z:]
	}
	var comments []string
	for line := range strings.SplitSeq(outside, "\n") {
		if at := tomlInlineCommentIndex(line); at >= 0 {
			comments = append(comments, line[at:])
		}
	}
	for _, b := range blocks {
		chunk := []byte(body[b.start+1 : b.end])
		depth := 0
		if err := scanTOMLOutsideStrings(string(chunk), 0, len(chunk), func(pos int, ch byte) bool {
			switch ch {
			case '[', '{':
				depth++
			case ']', '}':
				depth--
			case ',':
				if depth == 0 {
					chunk[pos] = '\n'
				}
			}
			return true
		}); err != nil {
			return body, err
		}
		tables.WriteString("\n[[providers]]\n" + string(chunk) + "\n")
	}
	return body[:assignment] + strings.Join(comments, "\n") + body[end+1:] + tables.String(), nil
}

func patchOpenCodeGoProvider(raw string, old, next ProviderEntry) (string, error) {
	ov, nv := reflect.ValueOf(old), reflect.ValueOf(next)
	for _, field := range []string{"Name", "Kind", "BaseURL", "RequestURL", "ChatURL", "Models", "Model", "Default", "PresetID", "PresetVersion", "ResponsesMode", "Effort"} {
		a, b := ov.FieldByName(field).Interface(), nv.FieldByName(field).Interface()
		if reflect.DeepEqual(a, b) {
			continue
		}
		f, _ := ov.Type().FieldByName(field)
		var err error
		raw, err = rawTOMLSet(raw, []string{strings.Split(f.Tag.Get("toml"), ",")[0]}, b)
		if err != nil {
			return raw, err
		}
	}
	for model, o := range next.ModelOverrides {
		previous := old.ModelOverrides[model]
		for _, pair := range []struct {
			key  string
			a, b any
		}{{"reasoning_protocol", previous.ReasoningProtocol, o.ReasoningProtocol}, {"supported_efforts", previous.SupportedEfforts, o.SupportedEfforts}, {"default_effort", previous.DefaultEffort, o.DefaultEffort}} {
			if reflect.DeepEqual(pair.a, pair.b) {
				continue
			}
			var err error
			raw, err = rawTOMLSet(raw, []string{"providers", "model_overrides", model, pair.key}, pair.b)
			if err != nil {
				return raw, err
			}
		}
	}
	return raw, nil
}

func rewriteOpenCodeGoReferences(body string, before, after *Config) (string, error) {
	// Update only reference fields whose resolved migration changed them.
	var err error
	for _, pair := range []struct {
		path []string
		a, b any
	}{
		{[]string{"config_version"}, before.ConfigVersion, openCodeGoUpgradeVersion},
		{[]string{"default_model"}, before.DefaultModel, after.DefaultModel},
		{[]string{"desktop", "provider_access"}, before.Desktop.ProviderAccess, after.Desktop.ProviderAccess},
		{[]string{"agent", "planner_model"}, before.Agent.PlannerModel, after.Agent.PlannerModel},
		{[]string{"agent", "vision_model"}, before.Agent.VisionModel, after.Agent.VisionModel},
		{[]string{"agent", "guardian_model"}, before.Agent.GuardianModel, after.Agent.GuardianModel},
		{[]string{"agent", "recovery_model"}, before.Agent.RecoveryModel, after.Agent.RecoveryModel},
		{[]string{"agent", "subagent_model"}, before.Agent.SubagentModel, after.Agent.SubagentModel},
		{[]string{"agent", "web_search_model"}, before.Agent.WebSearchModel, after.Agent.WebSearchModel},
		{[]string{"bot", "model"}, before.Bot.Model, after.Bot.Model},
	} {
		if reflect.DeepEqual(pair.a, pair.b) {
			continue
		}
		body, err = rawTOMLSet(body, pair.path, pair.b)
		if err != nil {
			return body, err
		}
	}
	for name, ref := range after.Agent.SubagentModels {
		if before.Agent.SubagentModels[name] != ref {
			body, err = rawTOMLSet(body, []string{"agent", "subagent_models", name}, ref)
			if err != nil {
				return body, err
			}
		}
	}
	// Bot array tables need positional edits, never a global string replace.
	botIndex, inBotConnection := -1, false
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if tomlSectionHeader(line) == "bot.connections" && strings.HasPrefix(strings.TrimSpace(line), "[[") {
			botIndex++
			inBotConnection = true
			continue
		}
		if inBotConnection && botIndex >= 0 && botIndex < len(after.Bot.Connections) && isTOMLKeyAssignment(line, "model") && before.Bot.Connections[botIndex].Model != after.Bot.Connections[botIndex].Model {
			lines[i] = replaceTOMLStringAssignment(line, after.Bot.Connections[botIndex].Model)
		}
		if h := tomlSectionHeader(line); h != "" {
			inBotConnection = false
		}
	}
	body = strings.Join(lines, "\n")
	return body, nil
}
