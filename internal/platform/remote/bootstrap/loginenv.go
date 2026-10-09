package bootstrap

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"reasonix/internal/platform/remote"
)

// EnvOutcome says what asking the account's own shell for its environment
// produced. Callers branch on it; nothing reads the shell's words.
type EnvOutcome int

const (
	// EnvUnavailable: no usable answer — no shell resolved, the shell ran and
	// said nothing parseable, the transport failed, or the value was rejected.
	EnvUnavailable EnvOutcome = iota
	// EnvCaptured: the PATH the user's terminal sees.
	EnvCaptured
	// EnvNonPOSIX: the account's shell cannot evaluate POSIX arithmetic, so
	// the capture script means nothing to it; commands keep the exec PATH.
	EnvNonPOSIX
)

// Bounds on what a remote rc file can make this side hold or run.
const (
	envOutputLimit  = 16 << 10
	envPathLimit    = 8 << 10
	envMaxEntries   = 256
	envCaptureLimit = 10 * time.Second
)

// loginEnv is the remote user's terminal environment, as far as bootstrap uses
// it: the PATH entries, and why there are none when there are none.
type loginEnv struct {
	Outcome EnvOutcome
	Path    []string
}

// The nonce only separates the block from rc noise; it is not authentication.
var newEnvNonce = func() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// captureScript runs under sh whatever the account's shell is: it resolves
// that shell, runs one interactive login command that prints PATH between
// nonce-keyed lines, and keeps only the output's tail. Silence is split into
// "cannot evaluate POSIX" and "ran and failed" by a non-interactive check.
const captureScript = `N=%[1]s
S=$(getent passwd "$(id -un)" 2>/dev/null | cut -d: -f7)
[ -x "$S" ] || S=${SHELL:-}
[ -x "$S" ] || { printf '%%s:noshell\n' "$N"; exit 0; }
OUT=$("$S" -i -l -c 'printf "\n%%s:arith=%%s\n%%s:path=%%s\n" %[1]s "$((6*7))" %[1]s "$PATH"' </dev/null 2>/dev/null | tail -c %[2]d)
case "$OUT" in
*"$N:path="*) printf '%%s\n' "$OUT" ;;
*) if [ "$("$S" -c 'echo $((6*7))' </dev/null 2>/dev/null)" = 42 ]; then printf '%%s:failed\n' "$N"; else printf '%%s:nonposix\n' "$N"; fi ;;
esac
exit 0`

// captureLoginEnv spends one round trip. It never fails: a connection that
// cannot answer yields EnvUnavailable and keeps today's behaviour.
func captureLoginEnv(ctx context.Context, conn Conn) loginEnv {
	nonce := newEnvNonce()
	ctx, cancel := context.WithTimeout(ctx, envCaptureLimit)
	defer cancel()
	res, err := conn.Exec(ctx, "sh -c "+shellQuote(fmt.Sprintf(captureScript, nonce, envOutputLimit)))
	if err != nil {
		return loginEnv{}
	}
	out := res.Stdout
	if len(out) > envOutputLimit {
		out = out[len(out)-envOutputLimit:]
	}
	return parseLoginEnv(strings.ReplaceAll(string(out), "\r\n", "\n"), nonce)
}

func parseLoginEnv(out, nonce string) loginEnv {
	if strings.Contains(out, "\n"+nonce+":nonposix\n") || strings.HasPrefix(out, nonce+":nonposix\n") {
		return loginEnv{Outcome: EnvNonPOSIX}
	}
	pathKey, arithKey := nonce+":path=", nonce+":arith=42"
	lines := strings.Split(out, "\n")
	for i := len(lines) - 1; i > 0; i-- {
		raw, ok := strings.CutPrefix(lines[i], pathKey)
		if !ok {
			continue
		}
		if lines[i-1] != arithKey {
			return loginEnv{}
		}
		if path := cleanPath(raw); len(path) > 0 {
			return loginEnv{Outcome: EnvCaptured, Path: path}
		}
		return loginEnv{}
	}
	return loginEnv{}
}

// cleanPath keeps absolute, printable entries. A relative or empty entry means
// the current directory, which the commands run from is not the user's.
func cleanPath(raw string) []string {
	if len(raw) > envPathLimit || strings.ContainsFunc(raw, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return nil
	}
	var out []string
	for e := range strings.SplitSeq(raw, ":") {
		if strings.HasPrefix(e, "/") {
			out = append(out, e)
		}
		if len(out) == envMaxEntries {
			break
		}
	}
	return out
}

// envConn runs every command with the captured PATH in front of the one the
// exec channel had. The value is single-quoted, so it is data to the shell.
type envConn struct {
	Conn
	env loginEnv
}

func (c *envConn) Exec(ctx context.Context, cmd string) (remote.ExecResult, error) {
	return c.Conn.Exec(ctx, "export PATH="+shellQuote(strings.Join(c.env.Path, ":"))+`:"$PATH"; `+cmd)
}

// withLoginEnv wraps conn only when the capture produced a PATH.
func withLoginEnv(env loginEnv, conn Conn) Conn {
	if env.Outcome != EnvCaptured {
		return conn
	}
	return &envConn{Conn: conn, env: env}
}

// connWithLoginEnv captures once and wraps the connection for POSIX targets.
// Windows is left alone: its commands run in PowerShell and have no rc files.
func connWithLoginEnv(ctx context.Context, conn Conn, target remoteOS) (Conn, loginEnv) {
	if _, ok := conn.(*envConn); ok {
		return conn, conn.(*envConn).env
	}
	if _, posix := target.(posixShell); !posix {
		return conn, loginEnv{}
	}
	env := captureLoginEnv(ctx, conn)
	return withLoginEnv(env, conn), env
}

// NPMUnavailableError is ErrNPMUnavailable with what was searched, as data:
// the PATH entries the captured login environment offered, and whether one
// was captured at all. Callers match with errors.Is(err, ErrNPMUnavailable).
type NPMUnavailableError struct {
	// Searched is remote-controlled text: it may hold control characters, bidi
	// marks or invalid UTF-8, so any renderer must escape it.
	Searched []string
	Outcome  EnvOutcome
	Err      error
}

func (e *NPMUnavailableError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%v: %v", ErrNPMUnavailable, e.Err)
	}
	return ErrNPMUnavailable.Error()
}

func (e *NPMUnavailableError) Unwrap() error { return e.Err }

func (e *NPMUnavailableError) Is(target error) bool { return target == ErrNPMUnavailable }

func npmUnavailable(env loginEnv, cause error) error {
	return &NPMUnavailableError{Searched: env.Path, Outcome: env.Outcome, Err: cause}
}
