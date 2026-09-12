package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"reasonix/internal/permissionpreset"
	"reasonix/internal/sandbox"
	"reasonix/internal/tool"
)

func (bash) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"Shell command to execute"},"run_in_background":{"type":"boolean","description":"Run detached: returns a job id immediately and keeps running across turns (no foreground timeout). Read new output with bash_output, wait with wait, stop it with kill_shell. Use for long-running commands like servers, watchers, or builds you don't need to block on."},"preserve_background_processes":{"type":"boolean","description":"After the shell command exits normally, keep any process-group members it intentionally left behind. Use only for deliberate daemonization, browser/GUI/session launchers such as playwright-cli open, or nohup/disown/setsid; cancellation and timeouts still kill the process group."},"additional_write_dirs":{"type":"array","items":{"type":"string"},"description":"Directories this command must write outside the workspace. Directories only, no globs. Accepts absolute paths, workspace-relative paths, ~, and ${HOME}. Request the smallest set needed; the host will not infer paths from the command text."},"sandbox_permissions":{"type":"string","enum":["workspace-write","danger-full-access"],"description":"Optional per-call permission escalation. Use workspace-write for an authorized write while the session is read-only. danger-full-access is accepted only after a host-recorded denial and explicit authorization."},"justification":{"type":"string","description":"Required when additional_write_dirs or sandbox_permissions is set. Explain why the access is needed."},"denial_id":{"type":"string","description":"Host-issued denial identifier required when retrying with danger-full-access."}},"required":["command"]}`)
}

func (b bash) DeclareWriteAccess(args json.RawMessage) (tool.WriteAccessDeclaration, error) {
	var p bashParams
	if err := json.Unmarshal(args, &p); err != nil {
		return tool.WriteAccessDeclaration{}, fmt.Errorf("invalid args: %w", err)
	}
	if err := validateBashWriteDirs(p); err != nil {
		return tool.WriteAccessDeclaration{}, err
	}
	return tool.WriteAccessDeclaration{
		Directories:     append([]string(nil), p.AdditionalWriteDirs...),
		Justification:   strings.TrimSpace(p.Justification),
		RequestedPreset: strings.TrimSpace(p.SandboxPermissions),
		DenialID:        strings.TrimSpace(p.DenialID),
	}, nil
}

func validateBashWriteDirs(p bashParams) error {
	preset := strings.TrimSpace(p.SandboxPermissions)
	if preset != "" && preset != string(permissionpreset.WorkspaceWrite) && preset != string(permissionpreset.DangerFullAccess) {
		return fmt.Errorf("sandbox_permissions must be workspace-write or danger-full-access")
	}
	if preset != "" && strings.TrimSpace(p.Justification) == "" {
		return fmt.Errorf("justification is required when sandbox_permissions is set")
	}
	if preset == string(permissionpreset.DangerFullAccess) && strings.TrimSpace(p.DenialID) == "" {
		return fmt.Errorf("denial_id is required when sandbox_permissions is danger-full-access")
	}
	if len(p.AdditionalWriteDirs) == 0 {
		return nil
	}
	if strings.TrimSpace(p.Justification) == "" {
		return fmt.Errorf("justification is required when additional_write_dirs is set")
	}
	for _, dir := range p.AdditionalWriteDirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			return fmt.Errorf("additional_write_dirs entries must be non-empty directories")
		}
		if strings.ContainsAny(dir, "*?[") {
			return fmt.Errorf("additional_write_dirs %q must be a concrete directory, not a glob", dir)
		}
	}
	return nil
}

func validateBashParams(p bashParams) error {
	if p.Command == "" {
		return fmt.Errorf("command is required")
	}
	return validateBashWriteDirs(p)
}

func bashPreflightFailure(ex *tool.ShellExecution, start time.Time, err error) (tool.DetailedResult, error) {
	ex.State = tool.ShellStateNotRun
	ex.FailurePhase = tool.ShellPhasePreflight
	ex.MutationRisk = tool.ShellMutationNotStarted
	ex.DurationMs = time.Since(start).Milliseconds()
	return tool.DetailedResult{Execution: ex}, err
}

func (b bash) appendWriteHints(ctx context.Context, out string, err error, p bashParams, wrapped bool) string {
	out = appendSessionDataHint(out, b.guard.CommandHint(b.workDir, p.Command))
	if wrapped {
		out = appendSandboxWriteHint(out, err, p, b.specForCall(ctx), string(sandbox.PermissionPresetFrom(ctx)))
	}
	return out
}

func (b bash) specForCall(ctx context.Context) sandbox.Spec {
	spec := b.sb
	preset := sandbox.PermissionPresetFrom(ctx)
	switch preset {
	case permissionpreset.ReadOnly:
		spec.Mode = "enforce"
		spec.ReadOnly = true
		spec.WriteRoots = nil
		spec.MinimalWrites = true
	case permissionpreset.WorkspaceWrite:
		// Permission presets own the enforcement decision. A legacy
		// [sandbox].bash="off" cannot silently turn workspace access into an
		// unconfined shell.
		spec.Mode = "enforce"
		spec.ReadOnly = false
		spec.MinimalWrites = true
		if len(spec.WriteRoots) == 0 && strings.TrimSpace(b.workDir) != "" {
			spec.WriteRoots = []string{b.workDir}
		}
	case permissionpreset.DangerFullAccess:
		spec.Mode = "off"
		spec.ReadOnly = false
	}
	if preset == permissionpreset.WorkspaceWrite {
		if b.rootSet != nil {
			spec.WriteRoots = b.rootSet.EffectiveSandboxRoots(ctx)
		} else if extra := sandbox.PerCallWriteRoots(ctx); len(extra) > 0 {
			spec.WriteRoots = sandbox.CollapseWriteRoots(append(append([]string{}, spec.WriteRoots...), extra...))
		}
	}
	if spec.ProtectedWriteRoots == nil && b.guard.stateRoot != "" {
		spec.ProtectedWriteRoots = sandbox.ProtectedWriteRoots(b.guard.stateRoot)
	}
	return spec
}

func bashWriteDeniedHint() string {
	return "The OS sandbox blocked a write outside the approved writable roots. Retry the same command with structured additional_write_dirs naming the exact directories (no globs), plus a justification. Example: {\"command\":\"mkdir -p ~/.local/bin && cp tool ~/.local/bin/tool\",\"additional_write_dirs\":[\"~/.local\"],\"justification\":\"install the user-requested local command\"}. Do not retry unconfined and do not omit the directories."
}

func looksLikeSandboxWriteDenial(out string, err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(out)
	if err != nil {
		msg += "\n" + strings.ToLower(err.Error())
	}
	for _, needle := range []string{
		"operation not permitted",
		"read-only file system",
		"erofs",
		"access is denied",
		"permissionerror: [errno 13] permission denied",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	// A bare "permission denied" can be an HTTP response or application-level
	// error. Accept it only in the standard local filesystem diagnostic shape
	// emitted by shells and file utilities.
	return localFilePermissionDenied.MatchString(msg)
}

var localFilePermissionDenied = regexp.MustCompile(`(?m)^(?:bash|zsh|sh|dash|fish|mkdir|touch|cp|mv|rm|ln|install|tee|cat|chmod|chown):[^\n]*permission denied\b`)

func appendSandboxWriteHint(out string, err error, p bashParams, spec sandbox.Spec, preset string) string {
	if !spec.Enforce() || len(p.AdditionalWriteDirs) > 0 || !looksLikeSandboxWriteDenial(out, err) {
		return out
	}
	hint := bashWriteDeniedHint()
	if denialID := sandbox.IssueDenial(p.Command, preset); denialID != "" {
		hint += " If the command cannot be expressed with additional_write_dirs, request danger-full-access for this exact retry with denial_id " + denialID + "."
	}
	return appendSessionDataHint(out, hint)
}
