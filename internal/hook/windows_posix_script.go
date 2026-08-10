package hook

import (
	"path/filepath"
	"strings"

	"reasonix/internal/pluginpkg"
)

func completePluginHookExecutionConfig(h pluginpkg.Hook, root, goos string, mode ExecutionMode) HookConfig {
	candidate := expandPluginRoot(h.Command, root)
	if candidate != "" && !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, filepath.FromSlash(candidate))
	}
	autoPOSIXScript := goos == "windows" && mode == ExecutionLegacy && h.Args == nil && isPOSIXShellScriptFile(candidate)
	if autoPOSIXScript {
		mode = ExecutionShell
		h.Shell = "bash"
	}
	explicitBash := goos == "windows" && mode == ExecutionShell && strings.EqualFold(strings.TrimSpace(h.Shell), "bash")
	expansionRoot := root
	if explicitBash {
		expansionRoot = strings.ReplaceAll(root, `\`, "/")
	}
	command := expandPluginRoot(h.Command, expansionRoot)
	resolveFromPluginRoot := (mode != ExecutionShell || autoPOSIXScript) &&
		!(mode == ExecutionExec && h.PayloadFormat == "claude")
	if command != "" && resolveFromPluginRoot && !filepath.IsAbs(command) {
		command = filepath.Join(root, filepath.FromSlash(command))
	}
	if mode == ExecutionLegacy {
		command = NormalizeCommand(command)
	} else if goos == "windows" && windowsShellMayUsePOSIXPath(h.Shell) {
		if scriptPath, ok := windowsPOSIXScriptPath(command, root); ok {
			h.Shell = "bash"
			command = bashSingleQuote(filepath.ToSlash(scriptPath))
		}
	}
	var argv []string
	if h.ArgsSet {
		argv = make([]string, 0, len(h.Args))
	}
	for _, arg := range h.Args {
		argv = append(argv, expandPluginRoot(arg, root))
	}
	return HookConfig{
		Command:       command,
		Argv:          argv,
		ExecutionMode: mode,
		Shell:         h.Shell,
	}
}

func bashSingleQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func windowsShellMayUsePOSIXPath(shell string) bool {
	switch strings.ToLower(strings.TrimSpace(shell)) {
	case "", "auto", "bash":
		return true
	default:
		return false
	}
}

func windowsPOSIXScriptPath(command, cwd string) (string, bool) {
	raw := strings.TrimSpace(command)
	rawPath := filepath.FromSlash(raw)
	if filepath.IsAbs(rawPath) && isPOSIXShellScriptFile(rawPath) {
		return filepath.Clean(rawPath), true
	}
	if len(raw) >= 2 && ((raw[0] == '"' && raw[len(raw)-1] == '"') || (raw[0] == '\'' && raw[len(raw)-1] == '\'')) {
		path := filepath.FromSlash(raw[1 : len(raw)-1])
		if filepath.IsAbs(path) && isPOSIXShellScriptFile(path) {
			return filepath.Clean(path), true
		}
	}
	fields, _, _, ok := parseSimpleHookCommandFields(command)
	if !ok || len(fields) != 1 {
		return "", false
	}
	path := filepath.FromSlash(fields[0])
	if !filepath.IsAbs(path) && strings.TrimSpace(cwd) != "" {
		path = filepath.Join(cwd, path)
	}
	if !isPOSIXShellScriptFile(path) {
		return "", false
	}
	return filepath.Clean(path), true
}

func normalizeWindowsHookSpawnInputForPlatform(in SpawnInput, goos string) SpawnInput {
	if goos != "windows" || in.Mode != ExecutionLegacy || in.Args != nil {
		return in
	}
	if scriptPath, ok := windowsPOSIXScriptPath(in.Command, in.Cwd); ok {
		in.Mode = ExecutionShell
		in.Shell = "bash"
		in.Command = bashSingleQuote(filepath.ToSlash(scriptPath))
	}
	return in
}

func requiresWindowsBashForHook(config HookConfig) bool {
	switch config.ExecutionMode {
	case ExecutionShell:
		if strings.EqualFold(strings.TrimSpace(config.Shell), "bash") {
			return true
		}
		return windowsShellMayUsePOSIXPath(config.Shell) && func() bool {
			_, ok := windowsPOSIXScriptPath(config.Command, config.Cwd)
			return ok
		}()
	case ExecutionExec:
		return isBarePOSIXShellWord(config.Command) && hasCommandStringFlag(config.Argv)
	case ExecutionLegacy:
		if config.Argv != nil {
			return isBarePOSIXShellWord(config.Command) && hasCommandStringFlag(config.Argv)
		}
		if _, ok := windowsPOSIXScriptPath(config.Command, config.Cwd); ok {
			return true
		}
		fields, _, _, ok := parseSimpleHookCommandFields(config.Command)
		return ok && len(fields) >= 3 && isBarePOSIXShellWord(fields[0]) && hasCommandStringFlag(fields[1:])
	default:
		return false
	}
}
