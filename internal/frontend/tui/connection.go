package tui

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/base/i18n"
	"reasonix/internal/frontend/termrender"
)

const connectionPickerRows = 8

// Connection is one model connection the setup panel offers.
type Connection struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Models      int    `json:"models"`
	Active      bool   `json:"active"`
	KeyRequired bool   `json:"keyRequired"`
}

// SetupState is whether the session still owes a key, so the panel can open
// itself on a first run.
type SetupState struct {
	Required bool   `json:"required"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}

func (c *Client) SetupState(ctx context.Context) (SetupState, error) {
	var out SetupState
	err := c.do(ctx, http.MethodGet, "/provider-setup", nil, &out)
	return out, err
}

// ConnectionList is the connections plus the credential-store revision a save
// must still find.
type ConnectionList struct {
	Revision    string       `json:"revision"`
	Connections []Connection `json:"connections"`
}

func (c *Client) Connections(ctx context.Context) (ConnectionList, error) {
	var out ConnectionList
	err := c.do(ctx, http.MethodGet, "/provider-setup/connections", nil, &out)
	return out, err
}

func (c *Client) TestConnection(ctx context.Context, provider, key string) error {
	return c.do(ctx, http.MethodPost, "/provider-setup/test", map[string]string{"provider": provider, "apiKey": key}, nil)
}

func (c *Client) SaveConnection(ctx context.Context, provider, key, revision string) error {
	return c.do(ctx, http.MethodPost, "/provider-setup/connection", map[string]string{"provider": provider, "apiKey": key, "revision": revision}, nil)
}

// connectionSetup is the "Configure connection" panel: a searchable list of
// connections, then a masked key field for the one chosen.
type connectionSetup struct {
	items    []Connection
	revision string
	query    string
	selected int // index into the filtered list

	provider    string // non-empty once a connection is chosen
	key         string
	saving      bool
	testing     bool
	testCancel  context.CancelFunc
	testVersion uint64
}

type (
	setupStateMsg struct {
		state SetupState
		err   error
	}
	connectionsMsg struct {
		list ConnectionList
		err  error
	}
	connectionTestedMsg struct {
		setup   *connectionSetup
		version uint64
		err     error
	}
	connectionSavedMsg struct {
		provider string
		err      error
	}
)

// checkSetup asks whether a key is owed; the answer opens the panel unasked.
func (m *model) checkSetup() tea.Cmd {
	return func() tea.Msg {
		s, err := m.client.SetupState(m.ctx)
		return setupStateMsg{state: s, err: err}
	}
}

func (m *model) openSetup() tea.Cmd {
	return func() tea.Msg {
		list, err := m.client.Connections(m.ctx)
		return connectionsMsg{list: list, err: err}
	}
}

// onSent handles a turn the kernel refused for want of a key: the line
// returns to the composer, since nothing was sent.
func (m *model) onSent(msg sentMsg) tea.Cmd {
	switch {
	case msg.err == nil:
		return nil
	case Code(msg.err) == CodeKeyMissing:
		if m.composer.Value() == "" {
			m.composer.SetValue(msg.display)
		}
		m.tr.AddNotice("warn", i18n.M.SetupTurnRefused)
	default:
		m.tr.AddNotice("error", "send: "+msg.err.Error())
	}
	return m.commit()
}

func (m *model) onSetupState(msg setupStateMsg) tea.Cmd {
	if msg.err != nil || !msg.state.Required || m.setup != nil {
		return nil
	}
	label := msg.state.Provider
	if msg.state.Model != "" {
		label += "/" + msg.state.Model
	}
	m.tr.AddNotice("warn", fmt.Sprintf(i18n.M.SetupOwed, label))
	return tea.Batch(m.commit(), m.openSetup())
}

func (m *model) onConnections(msg connectionsMsg) tea.Cmd {
	if msg.err != nil {
		m.tr.AddNotice("error", "setup: "+msg.err.Error())
		return m.commit()
	}
	if len(msg.list.Connections) == 0 {
		m.tr.AddNotice("warn", i18n.M.SetupNoConnections)
		return m.commit()
	}
	s := &connectionSetup{items: msg.list.Connections, revision: msg.list.Revision}
	for i, c := range msg.list.Connections {
		if c.Active {
			s.selected = i
		}
	}
	m.setup = s
	return nil
}

func (s *connectionSetup) filtered() []Connection {
	q := strings.ToLower(strings.TrimSpace(s.query))
	if q == "" {
		return s.items
	}
	var out []Connection
	for _, c := range s.items {
		if strings.Contains(strings.ToLower(c.Name+" "+connectionDescription(c)), q) {
			out = append(out, c)
		}
	}
	return out
}

func connectionDescription(c Connection) string {
	d := fmt.Sprintf(i18n.M.SetupModels, c.Kind, c.Models)
	if c.KeyRequired {
		d += " · " + i18n.M.SetupKeyRequired
	}
	return d
}

func (s *connectionSetup) invalidateTest() {
	if s.testCancel != nil {
		s.testCancel()
	}
	s.testing, s.testCancel = false, nil
	s.testVersion++
}

func (m *model) setupKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	s := m.setup
	if s == nil {
		return nil, false
	}
	if s.saving {
		return nil, true
	}
	if s.provider == "" {
		return m.pickConnectionKey(s, msg), true
	}
	return m.connectionKeyEdit(s, msg), true
}

func (m *model) pickConnectionKey(s *connectionSetup, msg tea.KeyPressMsg) tea.Cmd {
	items := s.filtered()
	k := msg.String()
	if s.query == "" {
		switch k {
		case "k":
			k = "up"
		case "j":
			k = "down"
		}
	}
	switch k {
	case "esc", "ctrl+c":
		m.setup = nil
	case "up", "ctrl+p":
		s.selected = max(s.selected-1, 0)
	case "down", "ctrl+n":
		s.selected = min(s.selected+1, max(len(items)-1, 0))
	case "enter":
		if s.selected >= 0 && s.selected < len(items) {
			s.provider = items[s.selected].Name
		}
	case "backspace":
		if s.query != "" {
			_, n := utf8.DecodeLastRuneInString(s.query)
			s.query, s.selected = s.query[:len(s.query)-n], 0
		}
	default:
		if t := typedText(msg); t != "" {
			s.query, s.selected = s.query+t, 0
		}
	}
	return nil
}

func (m *model) connectionKeyEdit(s *connectionSetup, msg tea.KeyPressMsg) tea.Cmd {
	switch k := msg.String(); k {
	case "esc", "ctrl+c":
		if s.testCancel != nil {
			s.testCancel()
		}
		m.setup = nil
	case "ctrl+t":
		return m.testConnection(s)
	case "backspace":
		if s.key != "" {
			s.invalidateTest()
			_, n := utf8.DecodeLastRuneInString(s.key)
			s.key = s.key[:len(s.key)-n]
		}
	case "enter":
		if s.key == "" {
			m.tr.AddNotice("warn", i18n.M.SetupNeedKey)
			return m.commit()
		}
		s.invalidateTest()
		s.saving = true
		provider, key, revision := s.provider, s.key, s.revision
		return func() tea.Msg {
			return connectionSavedMsg{provider: provider, err: m.client.SaveConnection(m.ctx, provider, key, revision)}
		}
	default:
		if t := typedText(msg); t != "" && !hasControl(t) {
			s.invalidateTest()
			s.key += t
		}
	}
	return nil
}

// pasteIntoSetup takes every paste while the panel is open: a key copied with
// its trailing newline must neither be dropped nor reach the hidden composer.
func (m *model) pasteIntoSetup(text string) {
	s := m.setup
	text = strings.TrimSpace(text)
	if s.provider == "" || s.saving || text == "" || strings.ContainsAny(text, "\r\n") || hasControl(text) {
		return
	}
	s.invalidateTest()
	s.key += text
}

func hasControl(s string) bool {
	return strings.IndexFunc(s, unicode.IsControl) >= 0
}

func typedText(msg tea.KeyPressMsg) string {
	if msg.Text != "" {
		return msg.Text
	}
	if s := msg.String(); len(s) == 1 && s[0] >= 32 && s[0] < 127 {
		return s
	}
	return ""
}

func (m *model) testConnection(s *connectionSetup) tea.Cmd {
	if s.testing {
		return nil
	}
	if s.key == "" {
		m.tr.AddNotice("warn", i18n.M.SetupNeedKeyToTest)
		return m.commit()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	s.testVersion++
	version, provider, key := s.testVersion, s.provider, s.key
	s.testing, s.testCancel = true, cancel
	return func() tea.Msg {
		return connectionTestedMsg{setup: s, version: version, err: m.client.TestConnection(ctx, provider, key)}
	}
}

func (m *model) onConnectionTested(msg connectionTestedMsg) tea.Cmd {
	if m.setup == nil || m.setup != msg.setup || m.setup.testVersion != msg.version {
		return nil
	}
	m.setup.testing, m.setup.testCancel = false, nil
	if msg.err != nil {
		m.tr.AddNotice("error", fmt.Sprintf(i18n.M.SetupTestFailed, msg.err))
	} else {
		m.tr.AddNotice("info", i18n.M.SetupTestOK)
	}
	return m.commit()
}

func (m *model) onConnectionSaved(msg connectionSavedMsg) tea.Cmd {
	if msg.err != nil {
		if m.setup != nil {
			m.setup.saving = false
		}
		m.tr.AddNotice("error", "setup: "+msg.err.Error())
		return m.commit()
	}
	m.setup = nil
	m.tr.AddNotice("info", fmt.Sprintf(i18n.M.SetupSaved, msg.provider))
	return tea.Batch(m.commit(), m.fetchStatus())
}

func (m *model) setupPanel() []string {
	s := m.setup
	if s.provider != "" {
		return m.setupKeyPanel(s)
	}
	items := s.filtered()
	s.selected = min(s.selected, max(len(items)-1, 0))
	lines := []string{termrender.Accent(i18n.M.SetupTitle)}
	if s.query != "" {
		lines = append(lines, "  "+termrender.Dim("Search: ")+s.query)
	}
	if len(items) == 0 {
		lines = append(lines, termrender.Dim("  "+i18n.M.SetupNoMatches))
	} else {
		start := 0
		if len(items) > connectionPickerRows {
			start = min(max(s.selected-connectionPickerRows/2, 0), len(items)-connectionPickerRows)
		}
		end := min(start+connectionPickerRows, len(items))
		if start > 0 {
			lines = append(lines, termrender.Dim("  ↑ more"))
		}
		for i := start; i < end; i++ {
			c := items[i]
			label := c.Name
			if c.Active {
				label += " " + termrender.Dim("("+i18n.M.SetupActive+")")
			}
			lines = append(lines, rowLine(i == s.selected, i+1, "", label, c.Active),
				termrender.Dim("     "+connectionDescription(c)))
		}
		if end < len(items) {
			lines = append(lines, termrender.Dim("  ↓ more"))
		}
	}
	lines = append(lines, termrender.Dim(i18n.M.SetupPickHint))
	return panel(lines, m.width, accentEdge)
}

func (m *model) setupKeyPanel(s *connectionSetup) []string {
	masked := strings.Repeat("•", utf8.RuneCountInString(s.key))
	if masked == "" {
		masked = termrender.Dim(i18n.M.SetupEnterCredential)
	}
	hint := i18n.M.SetupKeyHint
	switch {
	case s.saving:
		hint = i18n.M.SetupSaving
	case s.testing:
		hint = i18n.M.SetupTesting
	}
	return panel([]string{
		termrender.Accent(fmt.Sprintf(i18n.M.SetupConfigure, s.provider)),
		"  " + i18n.M.SetupAPIKey,
		"  " + termrender.Truncate(masked, max(m.width-8, 12), "…"),
		termrender.Dim(hint),
	}, m.width, accentEdge)
}
