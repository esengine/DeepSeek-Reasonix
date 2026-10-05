package builtin

import (
	"encoding/base64"
	"encoding/binary"
	"os/exec"
	"strings"
	"unicode/utf16"

	"reasonix/internal/base/shellparse"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/safety/shellsafe"
)

func deleteInterpreter(call shellparse.DeleteCall, sh sandbox.Shell) (string, sandbox.Shell, bool) {
	base, args, known := shellsafe.UnwrapDeleteCommand(call.Name, call.Args)
	if !known {
		return "", sh, false
	}
	switch base {
	case "iex", "invoke-expression":
		if len(args) == 1 {
			return args[0], sh, true
		}
		if len(args) == 2 && strings.EqualFold(args[0], "-Command") {
			return args[1], sh, true
		}
		return "", sh, true
	case "powershell", "pwsh":
		inner := sh
		if sh.Kind != sandbox.ShellPowerShell {
			path, _ := exec.LookPath(base)
			inner = sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: path}
		}
		payload, ok := powerShellInterpreterPayload(args)
		return payload, inner, ok
	case "bash", "sh":
		inner := sandbox.Shell{Kind: sandbox.ShellBash, Path: base}
		for i, arg := range args {
			if arg == "--" {
				break
			}
			if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.Contains(arg[1:], "c") {
				if i+1 < len(args) {
					return args[i+1], inner, true
				}
				return "", inner, true
			}
		}
	}
	return "", sh, false
}

func powerShellInterpreterPayload(args []string) (string, bool) {
	for i, arg := range args {
		flag := strings.ToLower(arg)
		switch flag {
		case "-command", "-c":
			parts := append([]string(nil), args[i+1:]...)
			for j, part := range parts {
				if part == "" {
					parts[j] = "$__reasonix_unknown"
				}
			}
			return strings.Join(parts, " "), true
		case "-encodedcommand", "-enc":
			if len(args) != i+2 {
				return "", true
			}
			return decodePowerShellPayload(args[i+1]), true
		}
	}
	return "", false
}

func decodePowerShellPayload(value string) string {
	raw, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(raw)%2 != 0 {
		return ""
	}
	units := make([]uint16, len(raw)/2)
	for j := range units {
		units[j] = binary.LittleEndian.Uint16(raw[j*2:])
	}
	return string(utf16.Decode(units))
}
