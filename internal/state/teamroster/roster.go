package teamroster

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// SchemaVersion is the roster shape this build writes.
const SchemaVersion = 1

// Field limits, in runes.
const (
	MaxNameRunes         = 40
	MaxRoleRunes         = 24
	MaxInstructionsRunes = 8000
	MaxGroupNameRunes    = 60
	MaxNoticeRunes       = 2000
)

// Group size and relay bounds.
const (
	MaxAgents     = 32
	MaxGroups     = 16
	MinMembers    = 2
	MaxMembers    = 12
	MaxRelayLimit = 200
)

// ByUser is the CreatedBy of a record the user made rather than an agent.
const ByUser = "user"

// Mode decides who answers a user's message in a group that names no one.
type Mode string

const (
	ModeAdmin    Mode = "admin"
	ModeEveryone Mode = "everyone"
)

var (
	agentID = regexp.MustCompile(`^a-[0-9a-f]{16}$`)
	groupID = regexp.MustCompile(`^g-[0-9a-f]{16}$`)
	token   = regexp.MustCompile(`^[a-z0-9-]{0,24}$`)
)

// Avatar is how an agent is drawn. Glyph and Color are presentation tokens the
// frontend resolves, an empty one its derived default; Image names a photo in
// AvatarDir, worn over the glyph when set.
type Avatar struct {
	Glyph string `json:"glyph,omitempty"`
	Color string `json:"color,omitempty"`
	Image string `json:"image,omitempty"`
}

// Part is an earlier session of an agent, closed when its conversation was
// restarted.
type Part struct {
	SessionPath string    `json:"sessionPath"`
	EndedAt     time.Time `json:"endedAt"`
}

// Agent is one long-lived member of the team. Its model is owned by its
// session's sidecar once that exists; InitialModel only seeds the first open.
// Permission is the approval mode the user gave it; empty follows the window.
type Agent struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Role         string    `json:"role,omitempty"`
	Instructions string    `json:"instructions,omitempty"`
	Avatar       Avatar    `json:"avatar"`
	Composition  string    `json:"composition,omitempty"`
	InitialModel string    `json:"initialModel,omitempty"`
	Folder       string    `json:"folder,omitempty"`
	Permission   string    `json:"permission,omitempty"`
	SessionPath  string    `json:"sessionPath,omitempty"`
	Parts        []Part    `json:"parts,omitempty"`
	Main         bool      `json:"main,omitempty"`
	Pinned       bool      `json:"pinned,omitempty"`
	Hidden       bool      `json:"hidden,omitempty"`
	CreatedBy    string    `json:"createdBy"`
	CreatedAt    time.Time `json:"createdAt"`
}

// Group is a chat several agents take turns in. MemberSessions maps a member
// to the session it speaks through in this group; RelayLimit 0 is the default.
// An archived group is kept for its history and takes no new posts.
type Group struct {
	ID             string            `json:"id"`
	Name           string            `json:"name,omitempty"`
	Notice         string            `json:"notice,omitempty"`
	Members        []string          `json:"members"`
	Admin          string            `json:"admin,omitempty"`
	Mode           Mode              `json:"mode"`
	RelayLimit     int               `json:"relayLimit,omitempty"`
	Folder         string            `json:"folder,omitempty"`
	MemberSessions map[string]string `json:"memberSessions,omitempty"`
	Pinned         bool              `json:"pinned,omitempty"`
	Hidden         bool              `json:"hidden,omitempty"`
	Archived       bool              `json:"archived,omitempty"`
	CreatedBy      string            `json:"createdBy"`
	CreatedAt      time.Time         `json:"createdAt"`
}

// Roster is the whole team as one file holds it.
type Roster struct {
	SchemaVersion int     `json:"schemaVersion"`
	Revision      uint64  `json:"revision"`
	Agents        []Agent `json:"agents"`
	Groups        []Group `json:"groups"`
}

func newAgentID() string { return "a-" + randomHex() }

func newGroupID() string { return "g-" + randomHex() }

// AddAgent appends a, under a fresh id, and returns it as stored. The roster
// is checked when it is written, not here.
func (r *Roster) AddAgent(a Agent) Agent {
	a.ID = newAgentID()
	r.Agents = append(r.Agents, a)
	return a
}

// AddGroup appends g, under a fresh id, and returns it as stored.
func (r *Roster) AddGroup(g Group) Group {
	g.ID = newGroupID()
	r.Groups = append(r.Groups, g)
	return g
}

func randomHex() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("teamroster: crypto/rand: %v", err))
	}
	return hex.EncodeToString(b[:])
}

// Agent returns the agent with id.
func (r *Roster) Agent(id string) (*Agent, bool) {
	for i := range r.Agents {
		if r.Agents[i].ID == id {
			return &r.Agents[i], true
		}
	}
	return nil, false
}

// AgentNamed returns the agent whose name equals name, ignoring case.
func (r *Roster) AgentNamed(name string) (*Agent, bool) {
	name = strings.TrimSpace(name)
	for i := range r.Agents {
		if strings.EqualFold(r.Agents[i].Name, name) {
			return &r.Agents[i], true
		}
	}
	return nil, false
}

// Group returns the group with id.
func (r *Roster) Group(id string) (*Group, bool) {
	for i := range r.Groups {
		if r.Groups[i].ID == id {
			return &r.Groups[i], true
		}
	}
	return nil, false
}

// IsMember reports whether agent belongs to the group.
func (g *Group) IsMember(agent string) bool { return slices.Contains(g.Members, agent) }

func (r *Roster) normalize() {
	for i := range r.Agents {
		a := &r.Agents[i]
		a.Name, a.Role = strings.TrimSpace(a.Name), strings.TrimSpace(a.Role)
	}
	for i := range r.Groups {
		r.Groups[i].Name = strings.TrimSpace(r.Groups[i].Name)
	}
}

// Validate reports the first rule the roster breaks.
func (r *Roster) Validate() error {
	return r.validateWithin(MaxAgents, MaxGroups)
}

func (r *Roster) validateWithin(agents, groups int) error {
	if len(r.Agents) > agents {
		return ErrAgentCapacity
	}
	if len(r.Groups) > groups {
		return ErrGroupCapacity
	}
	names := map[string]bool{}
	ids := map[string]bool{}
	mains := 0
	for i := range r.Agents {
		a := &r.Agents[i]
		if !agentID.MatchString(a.ID) || ids[a.ID] {
			return fieldErr("agent.id", ErrInvalid)
		}
		ids[a.ID] = true
		if err := checkLine("name", a.Name, MaxNameRunes, true); err != nil {
			return err
		}
		if key := strings.ToLower(a.Name); names[key] {
			return fieldErr("name", ErrNameTaken)
		}
		names[strings.ToLower(a.Name)] = true
		if err := checkLine("role", a.Role, MaxRoleRunes, false); err != nil {
			return err
		}
		if utf8.RuneCountInString(a.Instructions) > MaxInstructionsRunes {
			return fieldErr("instructions", ErrTooLong)
		}
		if !token.MatchString(a.Avatar.Glyph) || !token.MatchString(a.Avatar.Color) {
			return fieldErr("avatar", ErrInvalid)
		}
		if a.Avatar.Image != "" && !avatarFile.MatchString(a.Avatar.Image) {
			return fieldErr("avatar.image", ErrInvalid)
		}
		if a.Folder != "" && (!filepath.IsAbs(a.Folder) || filepath.Clean(a.Folder) != a.Folder) {
			return fieldErr("folder", ErrInvalid)
		}
		switch a.Permission {
		case "", "readOnly", "dontAsk", "ask", "auto", "yolo":
		default:
			return fieldErr("permission", ErrInvalid)
		}
		if a.Main {
			mains++
		}
	}
	if len(r.Agents) > 0 && mains == 0 {
		return ErrLastMain
	}
	groupIDs, groupNames := map[string]bool{}, map[string]bool{}
	for i := range r.Groups {
		g := &r.Groups[i]
		if !groupID.MatchString(g.ID) || groupIDs[g.ID] {
			return fieldErr("group.id", ErrInvalid)
		}
		groupIDs[g.ID] = true
		if key := strings.ToLower(g.Name); g.Name != "" && groupNames[key] {
			return fieldErr("group_name", ErrNameTaken)
		}
		groupNames[strings.ToLower(g.Name)] = true
		if err := validateGroup(g, ids); err != nil {
			return err
		}
	}
	return nil
}

func validateGroup(g *Group, agents map[string]bool) error {
	if err := checkLine("group_name", g.Name, MaxGroupNameRunes, false); err != nil {
		return err
	}
	if utf8.RuneCountInString(g.Notice) > MaxNoticeRunes {
		return fieldErr("notice", ErrTooLong)
	}
	if len(g.Members) < MinMembers || len(g.Members) > MaxMembers {
		return ErrGroupSize
	}
	seen := map[string]bool{}
	for _, m := range g.Members {
		if !agents[m] {
			return ErrUnknownAgent
		}
		if seen[m] {
			return fieldErr("members", ErrInvalid)
		}
		seen[m] = true
	}
	if g.Admin != "" && !seen[g.Admin] {
		return ErrNotMember
	}
	switch g.Mode {
	case ModeEveryone:
	case ModeAdmin:
		if g.Admin == "" {
			return fieldErr("mode", ErrInvalid)
		}
	default:
		return fieldErr("mode", ErrInvalid)
	}
	if g.RelayLimit < 0 || g.RelayLimit > MaxRelayLimit {
		return fieldErr("relay_limit", ErrInvalid)
	}
	for m := range g.MemberSessions {
		if !seen[m] {
			return ErrNotMember
		}
	}
	return nil
}

// checkLine holds a one-line field to its limit; names reach the model inside
// tag attributes and the sidebar on one row, so control characters are refused.
func checkLine(field, s string, limit int, required bool) error {
	if s == "" {
		if required {
			return fieldErr(field, ErrEmpty)
		}
		return nil
	}
	if utf8.RuneCountInString(s) > limit {
		return fieldErr(field, ErrTooLong)
	}
	for _, c := range s {
		if unicode.IsControl(c) {
			return fieldErr(field, ErrInvalid)
		}
	}
	return nil
}
