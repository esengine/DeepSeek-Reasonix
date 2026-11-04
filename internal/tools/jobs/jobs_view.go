package jobs

import "strings"

// View is a read-only snapshot of a job for the status bar.
type View struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Label string `json:"label"`
	// Description is the short name the caller gave it for a person, Detail the
	// full command or task it runs; Label stays the short preview.
	Description string `json:"description,omitempty"`
	Detail      string `json:"detail,omitempty"`
	Status      string `json:"status"`
	StartedAt   int64  `json:"startedAt"` // unix milliseconds
	// How a job that has ended did: when, the exit status of a command (none for
	// a task), and for any job the last line it wrote.
	EndedAt  int64  `json:"endedAt,omitempty"`
	ExitCode *int   `json:"exitCode,omitempty"`
	Tail     string `json:"tail,omitempty"`
}

// Running returns a snapshot of the still-running jobs (for the status bar).
func (m *Manager) Running() []View {
	return m.RunningForSession("")
}

// RunningForSession returns still-running jobs owned by parentSession. Empty
// parentSession preserves the legacy unscoped behavior.
func (m *Manager) RunningForSession(parentSession string) []View {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []View
	for _, key := range m.order {
		j := m.jobs[key]
		if !sessionMatches(parentSession, j.SessionID) {
			continue
		}
		select {
		case <-j.done:
			continue
		default:
		}
		j.mu.Lock()
		// A requested stop keeps the job on the running surface until its done
		// channel closes, so a rebuild or workspace lease cannot see an idle runtime
		// while the process tree is still unwinding.
		out = append(out, View{ID: j.ID, Kind: j.Kind, Label: j.Label, Description: j.Present.Description, Detail: j.Present.Detail, Status: string(Running), StartedAt: j.times.started, Tail: lastLine(j.tail)})
		j.mu.Unlock()
	}
	return out
}

// EndedForSession returns the jobs of parentSession that have finished, oldest
// first. They stay until the session is purged, so a person can still see how
// each one ended after it left the running surface.
func (m *Manager) EndedForSession(parentSession string) []View {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []View
	for _, key := range m.order {
		j := m.jobs[key]
		if !sessionMatches(parentSession, j.SessionID) {
			continue
		}
		select {
		case <-j.done:
		default:
			continue
		}
		j.mu.Lock()
		v := View{ID: j.ID, Kind: j.Kind, Label: j.Label, Description: j.Present.Description, Detail: j.Present.Detail,
			Status: string(j.status), StartedAt: j.times.started, EndedAt: j.times.finished, ExitCode: j.artifact.exit, Tail: j.artifact.last}
		if v.ExitCode == nil && j.Kind == "bash" && j.status == Done {
			zero := 0
			v.ExitCode = &zero
		}
		j.mu.Unlock()
		out = append(out, v)
	}
	return out
}

const maxTailRunes = 200

// lastLine is the last non-blank line a job wrote, cut to a row's width.
func lastLine(tail []byte) string {
	lines := strings.Split(strings.TrimRight(strings.ToValidUTF8(string(tail), ""), " \t\r\n"), "\n")
	line := strings.TrimSpace(lines[len(lines)-1])
	if r := []rune(line); len(r) > maxTailRunes {
		return string(r[:maxTailRunes]) + "…"
	}
	return line
}

// Presentation is what a job is called, and what it runs in full, for a person.
type Presentation struct {
	Description string
	Detail      string
}

// StartOption sets what a job carries beyond its kind and label, before it is
// visible to anything else.
type StartOption func(*Job)

// MaxDetailRunes bounds the full command a job keeps for display, and
// MaxDescriptionRunes the one-line name the model gave it.
const (
	MaxDetailRunes      = 4096
	MaxDescriptionRunes = 200
)

func bounded(s string, limit int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= limit {
		return string(r)
	}
	return string(r[:limit]) + "…"
}

// WithPresentation names the job for a person and records what it runs in full.
func WithPresentation(description, detail string) StartOption {
	return func(j *Job) {
		j.Present.Description = bounded(description, MaxDescriptionRunes)
		j.Present.Detail = bounded(detail, MaxDetailRunes)
	}
}

func applyOptions(j *Job, opts []StartOption) {
	for _, opt := range opts {
		opt(j)
	}
}
