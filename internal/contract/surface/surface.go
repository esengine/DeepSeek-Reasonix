// Package surface names the frontend a turn ran behind. One vocabulary,
// because three consumers spell it and none of them owns it: the usage record
// on disk, the telemetry counter on the wire, and the label a hub stamps on
// every session it opens. Spelled in three places it drifts, and the drift is
// invisible — a turn attributed to the wrong frontend still adds up.
package surface

type Surface string

const (
	CLI     Surface = "cli"
	Desktop Surface = "desktop"
	Studio  Surface = "studio"
	Serve   Surface = "serve"
	Bot     Surface = "bot"
	Remote  Surface = "remote"
)

// Valid reports whether s names a surface this build knows.
func (s Surface) Valid() bool {
	switch s {
	case CLI, Desktop, Studio, Serve, Bot, Remote:
		return true
	}
	return false
}

func (s Surface) String() string { return string(s) }

// Or answers with s when it names a surface and fallback otherwise, which is
// how a caller says what an unset field could only have meant: a record queued
// before the field existed, or a host that never set it.
func (s Surface) Or(fallback Surface) Surface {
	if s.Valid() {
		return s
	}
	return fallback
}
