package evidence

import (
	"encoding/json"
	"strings"

	"reasonix/internal/shellsafe"
)

// ClassifyEffect returns the concrete effect profile for one invocation.
func ClassifyEffect(in EffectInput) EffectProfile {
	name := strings.ToLower(strings.TrimSpace(in.ToolName))
	args := json.RawMessage(in.Args)
	if name == "bash" || name == "shell" {
		return classifyBashEffect(args, in)
	}
	if in.Hint.Present && in.Hint.ReadOnly {
		return readOnlyProfile(targetsFrom(in, nil), ReasonHintReadOnly)
	}
	if in.StaticReadOnly || IsNonMutationMetaTool(in.ToolName) {
		return readOnlyProfile(targetsFrom(in, nil), ReasonReadOnly)
	}
	switch name {
	case "ask", "todo_write", "complete_step", "bash_output", "wait":
		return readOnlyProfile(nil, ReasonReadOnly)
	case "remember", "forget", "set_session_title", "kill_shell":
		return EffectProfile{Known: true, HostState: true, Reason: ReasonHostState}
	}
	profile := writerProfile(in)
	if in.Hint.Present {
		applyCallHint(&profile, in.Hint)
	}
	return profile
}

func classifyBashEffect(args json.RawMessage, in EffectInput) EffectProfile {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil {
		return opaqueProfile(in, ReasonUnknown)
	}
	command := stringField(fields, "command")
	effect := shellsafe.ClassifyBash(command)
	if effect.Certainty != shellsafe.EffectKnown {
		if bashCommandIsVerification(command) {
			return readOnlyProfile(nil, ReasonReadOnly)
		}
		// Unproven bash is a pathless workspace write. It is not an MCP opaque
		// writer: permission and the shell contract still own the invocation.
		return EffectProfile{
			WorkspaceWrite: true,
			ExecutesCode:   effect.ExecutesCode,
			UsesNetwork:    effect.UsesNetwork,
			Reason:         EffectReason(effect.Reason),
		}
	}
	profile := EffectProfile{
		Known:          true,
		ReadOnly:       effect.Writes == 0,
		WorkspaceWrite: effect.Writes&shellsafe.WriteWorkspaceContent != 0,
		RepoMetadata:   effect.Writes&shellsafe.WriteRepositoryMetadata != 0,
		HostState:      effect.Writes&shellsafe.WriteHostState != 0,
		ExternalState:  effect.Writes&shellsafe.WriteExternalState != 0,
		ExecutesCode:   effect.ExecutesCode,
		UsesNetwork:    effect.UsesNetwork,
		Reason:         EffectReason(commandEffectReason(effect)),
	}
	if profile.ReadOnly {
		profile.Reason = ReasonReadOnly
	} else if profile.ExternalState {
		profile.Reason = ReasonExternalState
	} else if profile.HostState {
		profile.Reason = ReasonHostState
	} else if profile.WorkspaceWrite {
		profile.Reason = ReasonWorkspaceWrite
	} else if profile.RepoMetadata {
		profile.Reason = ReasonRepoMetadata
	}
	applyBashShape(&profile, effect.CommandFamily, command)
	profile.Targets = bashTargets(profile)
	return profile
}

func applyBashShape(profile *EffectProfile, family, command string) {
	family = strings.ToLower(strings.TrimSpace(family))
	lower := strings.ToLower(command)
	switch {
	case family == "git push" || strings.HasPrefix(family, "git push"):
		profile.ExternalState = true
		profile.UsesNetwork = true
		if containsForceFlag(lower) {
			profile.Destructive = true
			profile.Irreversible = true
		}
	case family == "git clean" && profile.WorkspaceWrite:
		profile.Destructive = true
	case strings.Contains(family, "publish") || strings.Contains(family, "deploy"):
		profile.ExternalState = true
		profile.UsesNetwork = true
	}
	if strings.Contains(lower, "rm -rf") || strings.Contains(lower, "rm -fr") {
		profile.Destructive = true
		profile.Irreversible = true
	}
}

func containsForceFlag(command string) bool {
	for field := range strings.FieldsSeq(command) {
		switch field {
		case "-f", "--force", "--force-with-lease":
			return true
		}
	}
	return false
}

func bashTargets(p EffectProfile) []Target {
	switch {
	case p.ExternalState:
		return []Target{{Kind: TargetExternal}}
	case p.HostState:
		return []Target{{Kind: TargetHost}}
	case p.RepoMetadata && !p.WorkspaceWrite:
		return []Target{{Kind: TargetRepo}}
	default:
		return nil
	}
}

func writerProfile(in EffectInput) EffectProfile {
	paths := declaredPaths(in)
	if !in.Hint.Present && !in.Hint.Known && len(paths) == 0 && looksOpaqueName(in.ToolName) {
		return opaqueProfile(in, ReasonOpaqueWriter)
	}
	profile := EffectProfile{
		Known:          true,
		WorkspaceWrite: true,
		Reason:         ReasonWorkspaceWrite,
		Targets:        fileTargets(paths),
	}
	if in.Hint.Destructive || isDestructiveTool(in.ToolName) {
		profile.Destructive = true
		profile.Reason = ReasonDestructive
	}
	return profile
}

func applyCallHint(profile *EffectProfile, hint CallHint) {
	profile.Destructive = profile.Destructive || hint.Destructive
	profile.Privileged = profile.Privileged || hint.Privileged
	profile.UsesNetwork = profile.UsesNetwork || hint.UsesNetwork
	profile.ExecutesCode = profile.ExecutesCode || hint.ExecutesCode
	if hint.Destructive {
		profile.Reason = ReasonHintDestructive
	}
	if len(hint.Targets) > 0 && len(profile.Targets) == 0 {
		profile.Targets = fileTargets(hint.Targets)
	}
}

func readOnlyProfile(targets []Target, reason EffectReason) EffectProfile {
	return EffectProfile{Known: true, ReadOnly: true, Reason: reason, Targets: append([]Target(nil), targets...)}
}

func opaqueProfile(in EffectInput, reason EffectReason) EffectProfile {
	if reason == "" {
		reason = ReasonOpaqueWriter
	}
	return EffectProfile{
		WorkspaceWrite: true,
		Destructive:    in.Hint.Destructive,
		Privileged:     in.Hint.Privileged || looksPrivilegedName(in.ToolName),
		UsesNetwork:    in.Hint.UsesNetwork,
		ExecutesCode:   in.Hint.ExecutesCode,
		Targets:        fileTargets(declaredPaths(in)),
		Reason:         reason,
	}
}

func declaredPaths(in EffectInput) []string {
	var paths []string
	if len(in.ActualPaths) > 0 {
		paths = append(paths, in.ActualPaths...)
	} else {
		paths = append(paths, ToolCallPaths(in.Args)...)
	}
	if in.Hint.Present {
		paths = append(paths, in.Hint.Targets...)
	}
	return uniquePaths(paths)
}

func targetsFrom(in EffectInput, extra []string) []Target {
	paths := append(declaredPaths(in), extra...)
	return fileTargets(uniquePaths(paths))
}

func fileTargets(paths []string) []Target {
	if len(paths) == 0 {
		return nil
	}
	out := make([]Target, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		kind := TargetFile
		if strings.HasSuffix(p, "/") {
			kind = TargetDirectory
		}
		out = append(out, Target{Path: p, Kind: kind})
	}
	return out
}

func uniquePaths(paths []string) []string {
	seen := make(map[string]bool, len(paths))
	var out []string
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

func looksOpaqueName(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	return strings.HasPrefix(lower, "mcp__") || strings.HasPrefix(lower, "mcp-tool:")
}

func looksPrivilegedName(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	for _, hint := range highRiskToolHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

func isDestructiveTool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "delete_file", "delete_symbol", "remove_file":
		return true
	default:
		return false
	}
}
