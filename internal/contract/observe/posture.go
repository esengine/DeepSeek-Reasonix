package observe

// Name is the one posture this package defines.
const Name = "observe"

// Enforcement values: what actually holds the read-only line.
const (
	// EnforcementOSSandbox means the operating system confines what the run
	// can write and reach, in addition to the tool set.
	EnforcementOSSandbox = "os-sandbox+tool-filter"
	// EnforcementToolFilter means the tool set is the only barrier.
	EnforcementToolFilter = "tool-filter"
)

// Sandbox values for Posture.Sandbox.
const (
	SandboxEnforce = "enforce"
	SandboxNone    = "none"
)

// Posture is the metadata a run record carries about its confinement. The
// json names are the wire names a frontend reads.
type Posture struct {
	Name        string `json:"name"`
	Enforcement string `json:"enforcement"`
	Sandbox     string `json:"sandbox"`
	// RemoteContent is false: a result of this run is shown without loading
	// remote images or following links on its own.
	RemoteContent bool `json:"remote_content"`
}

// New states the posture for a platform where osSandbox says whether the
// operating system enforces confinement for shell-free tools' subprocesses.
func New(osSandbox bool) Posture {
	if osSandbox {
		return Posture{Name: Name, Enforcement: EnforcementOSSandbox, Sandbox: SandboxEnforce}
	}
	return Posture{Name: Name, Enforcement: EnforcementToolFilter, Sandbox: SandboxNone}
}
