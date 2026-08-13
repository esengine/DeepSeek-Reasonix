package sandbox

// Prepared is the unified launch plan for a sandboxed (or unconfined) command
// that may hold a session-private temporary directory lease. Callers must
// Release the lease after the process exits — including when Start fails.
type Prepared struct {
	// Argv is the final process argv (possibly wrapped by bwrap/sandbox-exec).
	Argv []string
	// Wrapped reports whether an OS sandbox wrapper was applied.
	Wrapped bool
	// SessionTemp is the absolute host path of the private temporary directory
	// (empty when the launch has no session temp).
	SessionTemp string
	// EnvOverrides are KEY=value pairs to merge into the child environment
	// (TMPDIR/TMP/TEMP). Nil when no session temp is bound.
	EnvOverrides []string
	// LinuxSandboxed is true when SessionTemp is mapped at virtual /tmp under
	// Linux bubblewrap; env overrides then point at /tmp.
	LinuxSandboxed bool
}

// PrepareShell builds argv for a shell command string. When sessionTemp is
// non-empty it is attached to a copy of spec so the platform wrapper can bind
// or allow that directory.
func PrepareShell(spec Spec, sh Shell, command, sessionTemp string) Prepared {
	spec = withSessionTemp(spec, sessionTemp)
	argv, wrapped := Command(spec, sh, command)
	linuxSB := wrapped && sessionTemp != "" && isLinux()
	return Prepared{
		Argv:           argv,
		Wrapped:        wrapped,
		SessionTemp:    sessionTemp,
		EnvOverrides:   SessionTempEnv(sessionTemp, linuxSB),
		LinuxSandboxed: linuxSB,
	}
}

// PrepareArgs builds argv for a raw argument vector (e.g. ripgrep).
func PrepareArgs(spec Spec, args []string, sessionTemp string) Prepared {
	spec = withSessionTemp(spec, sessionTemp)
	argv, wrapped := CommandArgs(spec, args)
	linuxSB := wrapped && sessionTemp != "" && isLinux()
	return Prepared{
		Argv:           argv,
		Wrapped:        wrapped,
		SessionTemp:    sessionTemp,
		EnvOverrides:   SessionTempEnv(sessionTemp, linuxSB),
		LinuxSandboxed: linuxSB,
	}
}

func withSessionTemp(spec Spec, sessionTemp string) Spec {
	if sessionTemp == "" {
		return spec
	}
	spec.SessionTemp = sessionTemp
	return spec
}
