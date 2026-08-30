package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/event"
	"reasonix/internal/i18n"
)

// elicitCard is the CLI's MCP elicitation prompt: a flat form (string/number/
// boolean/enum fields) or a URL confirmation, raised by a server mid-call.
// It mirrors the chooser's modality: keys drive it while set, free-typed
// answers borrow the composer, and Submit/Decline/Cancel map to the
// accept/decline/cancel actions.
type elicitCard struct {
	id      string
	server  string
	mode    string
	message string
	url     string

	fields []elicitField
	values []string
	bools  []bool
	cursor int // 0..len-1: a field; len: the Submit row
	typing bool
}

type elicitField struct {
	Key      string   `json:"-"`
	Label    string   `json:"title"`
	Type     string   `json:"type"`
	Enum     []string `json:"enum"`
	Required bool     `json:"-"`
	Default  any      `json:"default"`
}

func parseElicitFields(schema json.RawMessage) []elicitField {
	if len(schema) == 0 {
		return nil
	}
	var parsed struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(schema, &parsed); err != nil {
		return nil
	}
	required := map[string]bool{}
	for _, r := range parsed.Required {
		required[r] = true
	}
	fields := make([]elicitField, 0, len(parsed.Properties))
	for key, raw := range parsed.Properties {
		var f elicitField
		f.Key = key
		f.Label = key
		f.Required = required[key]
		if err := json.Unmarshal(raw, &f); err != nil {
			continue
		}
		if f.Label == "" {
			f.Label = key
		}
		fields = append(fields, f)
	}
	return fields
}

func newElicitCard(interaction event.MCPInteraction) *elicitCard {
	card := &elicitCard{
		id:      interaction.ID,
		server:  interaction.Server,
		mode:    interaction.Mode,
		message: interaction.Message,
		url:     interaction.URL,
		fields:  parseElicitFields(interaction.RequestedSchema),
	}
	card.values = make([]string, len(card.fields))
	card.bools = make([]bool, len(card.fields))
	for i, f := range card.fields {
		switch v := f.Default.(type) {
		case string:
			card.values[i] = v
			if f.Type == "boolean" {
				card.bools[i] = v == "true"
			}
		case bool:
			card.bools[i] = v
			if v {
				card.values[i] = "true"
			}
		case float64:
			card.values[i] = fmt.Sprintf("%v", v)
		}
	}
	return card
}

func (c *elicitCard) onSubmitRow() bool { return c.cursor >= len(c.fields) }

// content coerces the typed strings into JSON-typed values.
func (c *elicitCard) content() map[string]any {
	out := map[string]any{}
	for i, f := range c.fields {
		raw := strings.TrimSpace(c.values[i])
		switch f.Type {
		case "boolean":
			out[f.Key] = c.bools[i]
		case "number", "integer":
			var num float64
			if _, err := fmt.Sscanf(raw, "%g", &num); err == nil && raw != "" {
				out[f.Key] = num
			}
		default:
			if raw != "" {
				out[f.Key] = raw
			}
		}
	}
	return out
}

// startElicit opens the card for a server-initiated elicitation.
func (m *chatTUI) startElicit(interaction event.MCPInteraction) {
	m.finalizeStreamed()
	m.elicit = newElicitCard(interaction)
}

// clearElicitCard drops the card when its turn settles (timeout or cancel).
func (m *chatTUI) clearElicitCard() {
	if m.elicit != nil {
		m.elicit = nil
		m.refreshInputPlaceholder()
	}
}

// elicitKey handles one keystroke for the active card, including free-text
// typing (Enter confirms, Esc backs out). handled=false leaves the keystroke
// with the composer, matching the chooser's typing mode.
func (m chatTUI) elicitKey(msg tea.KeyPressMsg, cmds []tea.Cmd) (tea.Model, tea.Cmd, bool) {
	c := m.elicit
	if c == nil {
		return m, nil, false
	}
	if c.typing {
		switch msg.String() {
		case "enter":
			val := strings.TrimSpace(m.input.Value())
			m.input.Reset()
			c.typing = false
			m.refreshInputPlaceholder()
			if val != "" {
				c.values[c.cursor] = val
				c.cursor++
			}
			return m, finalize(m, cmds), true
		case "esc":
			c.typing = false
			m.input.Reset()
			m.refreshInputPlaceholder()
			return m, finalize(m, cmds), true
		}
		return m, nil, false
	}
	model, cmd := m.handleElicitKey(msg)
	return model, cmd, true
}

// handleElicitKey routes a non-typing keystroke to the active card.
func (m chatTUI) handleElicitKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	c := m.elicit
	key := msg.String()
	switch key {
	case "ctrl+c", "esc":
		return m.elicitAnswer("cancel", c.id)
	case "o":
		if c.mode == "url" {
			openElicitURL(c.url)
		}
		return m, nil
	case "enter":
		if c.mode == "url" || c.onSubmitRow() {
			return m.elicitAnswer("accept", c.id)
		}
		return m.elicitFieldKey(key)
	case "d":
		if c.mode == "url" || c.onSubmitRow() {
			return m.elicitAnswer("decline", c.id)
		}
		return m, nil
	case "tab", "right", "l", "down", "j":
		if c.cursor < len(c.fields) {
			c.cursor++
		}
		return m, nil
	case "shift+tab", "left", "h", "up", "k":
		if c.cursor > 0 {
			c.cursor--
		}
		return m, nil
	}
	return m.elicitFieldKey(key)
}

// elicitFieldKey handles a key inside one field (boolean toggle, enum pick,
// open free-text entry).
func (m chatTUI) elicitFieldKey(key string) (tea.Model, tea.Cmd) {
	c := m.elicit
	f := c.fields[c.cursor]
	switch {
	case f.Type == "boolean" && (key == " " || key == "space" || key == "enter" || key == "y" || key == "n"):
		c.bools[c.cursor] = !c.bools[c.cursor]
		c.values[c.cursor] = fmt.Sprintf("%v", c.bools[c.cursor])
	case len(f.Enum) > 0 && len(key) == 1 && key[0] >= '1' && key[0] <= '9':
		if idx := int(key[0] - '1'); idx < len(f.Enum) {
			c.values[c.cursor] = f.Enum[idx]
		}
	case len(f.Enum) == 0 && f.Type != "boolean" && key == "enter":
		c.typing = true
		m.input.Reset()
		m.input.SetHeight(1)
		m.refreshInputPlaceholder()
	}
	return m, nil
}

func (m chatTUI) elicitAnswer(action, id string) (tea.Model, tea.Cmd) {
	content := map[string]any(nil)
	if action == "accept" && m.elicit != nil && m.elicit.mode != "url" {
		content = m.elicit.content()
	}
	m.ctrl.AnswerMCPInteraction(id, action, content)
	m.elicit = nil
	m.refreshInputPlaceholder()
	return m, nil
}

// openElicitURL opens a url-mode target in the system browser; the card shows
// the server and host before this ever runs.
func openElicitURL(raw string) {
	if cmd, err := mcpOpenCommand(raw); err == nil {
		_ = cmd.Start()
	}
}

// renderElicit draws the pinned elicitation card.
func (m chatTUI) renderElicit() string {
	c := m.elicit
	if c == nil {
		return ""
	}
	w := max(m.width, 10)
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n", accent("MCP"), dim("· "+c.server))
	b.WriteString(wrapForViewport(c.message, w, activeCLITheme.info) + "\n")
	if c.mode == "url" {
		host := c.url
		if u := strings.SplitN(strings.TrimPrefix(strings.TrimPrefix(c.url, "https://"), "http://"), "/", 2); len(u) > 0 {
			host = u[0]
		}
		fmt.Fprintf(&b, "  %s\n", host)
		b.WriteString(dim(i18n.M.ElicitURLHint))
		return choicePanelStyle.Width(w).Render(b.String())
	}
	if len(c.fields) == 0 {
		b.WriteString(dim(i18n.M.ElicitConfirmOnly))
		return choicePanelStyle.Width(w).Render(b.String())
	}
	for i, f := range c.fields {
		marker := "  "
		label := f.Label
		if f.Required {
			label += " *"
		}
		value := strings.TrimSpace(c.values[i])
		if f.Type == "boolean" {
			value = fmt.Sprintf("%v", c.bools[i])
		} else if value == "" {
			value = dim(i18n.M.ElicitUnanswered)
		}
		if i == c.cursor {
			marker = accent("› ")
			label = accent(label)
		}
		fmt.Fprintf(&b, "%s%s: %s\n", marker, label, value)
	}
	if c.onSubmitRow() {
		fmt.Fprintf(&b, "%s\n", accent("  ["+i18n.M.ElicitSubmit+"]"))
		b.WriteString(dim(i18n.M.ElicitSubmitHint))
	} else {
		fmt.Fprintf(&b, "%s\n", dim("  ["+i18n.M.ElicitSubmit+"]"))
	}
	return choicePanelStyle.Width(w).Render(b.String())
}
