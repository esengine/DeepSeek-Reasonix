package tui

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/base/i18n"
)

// ModelEntry is one chat model the kernel can switch this session to.
type ModelEntry struct {
	Ref         string `json:"ref"`
	Provider    string `json:"provider"`
	DisplayName string `json:"displayName"`
	Model       string `json:"model"`
	Active      bool   `json:"active"`
}

func (c *Client) Models(ctx context.Context) ([]ModelEntry, error) {
	var out struct {
		Models []ModelEntry `json:"models"`
	}
	err := c.do(ctx, http.MethodGet, "/models", nil, &out)
	return out.Models, err
}

func (c *Client) SetModel(ctx context.Context, ref string) error {
	return c.do(ctx, http.MethodPost, "/model", map[string]string{"ref": ref}, nil)
}

type (
	modelsMsg struct {
		forProvider bool
		provider    string
		list        []ModelEntry
		err         error
	}
	modelSwitchedMsg struct {
		ref string
		err error
	}
)

// modelSlash answers /model and /provider here: the kernel's reply to them is
// a plain list, and choosing from it is this screen's panel.
func (m *model) modelSlash(display string) (tea.Cmd, bool) {
	name, arg, _ := strings.Cut(display, " ")
	arg = strings.TrimSpace(arg)
	if name != "/model" && name != "/provider" {
		return nil, false
	}
	m.composer.Reset()
	m.tr.AddEcho(display)
	switch {
	case name == "/model" && arg != "":
		return tea.Batch(m.commit(), m.switchModel(arg)), true
	case name == "/provider" && arg != "":
		return tea.Batch(m.commit(), m.fetchModels(true, arg)), true
	}
	return tea.Batch(m.commit(), m.fetchModels(name == "/provider", "")), true
}

func (m *model) fetchModels(byProvider bool, provider string) tea.Cmd {
	return func() tea.Msg {
		list, err := m.client.Models(m.ctx)
		return modelsMsg{forProvider: byProvider, provider: provider, list: list, err: err}
	}
}

func (m *model) switchModel(ref string) tea.Cmd {
	if m.tr.Running {
		m.tr.AddNotice("warn", i18n.M.ModelSwitchBusy)
		return m.commit()
	}
	m.tr.AddNotice("info", fmt.Sprintf(i18n.M.ModelSwitchingFmt, ref))
	return tea.Batch(m.commit(), func() tea.Msg {
		return modelSwitchedMsg{ref: ref, err: m.client.SetModel(m.ctx, ref)}
	})
}

func (m *model) onModelSwitched(msg modelSwitchedMsg) tea.Cmd {
	if msg.err != nil {
		m.tr.AddNotice("error", "model: "+msg.err.Error())
		return m.commit()
	}
	return m.fetchStatus()
}

func (m *model) onModels(msg modelsMsg) tea.Cmd {
	if msg.err != nil {
		m.tr.AddNotice("error", "model: "+msg.err.Error())
		return m.commit()
	}
	switch {
	case msg.provider != "":
		return m.openProviderModels(msg.provider, msg.list)
	case msg.forProvider:
		return m.openProviders(msg.list)
	}
	items := make([]chooseItem, 0, len(msg.list))
	for _, e := range msg.list {
		items = append(items, chooseItem{ID: e.Ref, Label: e.Ref, Desc: fmt.Sprintf(i18n.M.ModelProviderFmt, e.Provider), Active: e.Active})
	}
	if len(items) == 0 {
		m.tr.AddNotice("warn", i18n.M.NoConfiguredModels)
		return m.commit()
	}
	m.quick = newQuickPicker(i18n.M.PickModelTitle, items, func(it chooseItem) tea.Cmd { return m.switchModel(it.ID) })
	return nil
}

func (m *model) openProviders(list []ModelEntry) tea.Cmd {
	var items []chooseItem
	at := map[string]int{}
	counts := map[string]int{}
	for _, e := range list {
		i, ok := at[e.Provider]
		if !ok {
			i = len(items)
			at[e.Provider] = i
			items = append(items, chooseItem{ID: e.Provider, Label: e.Provider})
		}
		counts[e.Provider]++
		items[i].Active = items[i].Active || e.Active
	}
	for i := range items {
		items[i].Desc = fmt.Sprintf(i18n.M.ProviderModelsFmt, counts[items[i].ID])
	}
	if len(items) == 0 {
		m.tr.AddNotice("warn", i18n.M.NoConfiguredModels)
		return m.commit()
	}
	m.quick = newQuickPicker(i18n.M.PickProviderTitle, items, func(it chooseItem) tea.Cmd {
		return m.fetchModels(true, it.ID)
	})
	return nil
}

func (m *model) openProviderModels(name string, list []ModelEntry) tea.Cmd {
	var items []chooseItem
	for _, e := range list {
		if e.Provider == name {
			items = append(items, chooseItem{ID: e.Ref, Label: e.Model, Desc: e.Provider, Active: e.Active})
		}
	}
	switch len(items) {
	case 0:
		m.tr.AddNotice("warn", fmt.Sprintf(i18n.M.ProviderUnknownFmt, name))
		return m.commit()
	case 1:
		return m.switchModel(items[0].ID)
	}
	m.quick = newQuickPicker(fmt.Sprintf(i18n.M.ProviderPickLabel, name), items, func(it chooseItem) tea.Cmd { return m.switchModel(it.ID) })
	return nil
}
