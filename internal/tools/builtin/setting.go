package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"reasonix/internal/contract/tool"
)

// SettingTool lets the model look up the user's settings and ask for a change.
// A change is only ever applied after the user's own answer to a card the host
// words; the tool never writes on the model's say-so.
func SettingTool(link *tool.SettingsLink) tool.Tool { return &settingTool{link: link} }

type settingTool struct{ link *tool.SettingsLink }

func (*settingTool) Name() string { return "setting" }
func (*settingTool) Description() string {
	return "Find and change the user's own settings. op=list finds settings by words of their id (for example \"peers dispatch\"). op=propose asks the user to confirm one change: give the id, the new value and a short reason; nothing changes until the user answers, and the result says what changed or that they declined. op=open points the user at a setting that only they may change. Use it when a refusal ends with (fix: tool:setting ...), or when a tool you need is not offered because a switch is off. Do not change a setting the task did not call for."
}
func (*settingTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"op":{"type":"string","enum":["list","propose","open"]},"query":{"type":"string","description":"Words of the setting's id, for op=list"},"id":{"type":"string","description":"The setting's id, for propose and open"},"value":{"description":"The new value as JSON, for op=propose"},"reason":{"type":"string","description":"Why the change helps the user, shown on the card, for op=propose"}},"required":["op"],"additionalProperties":false}`)
}

// ReadOnly is true because the only write waits on the user's own answer to a
// host-worded card, which is the approval this tool would otherwise ask for.
func (*settingTool) ReadOnly() bool { return true }

// ReadOnlyExecutionHostMutation keeps the tool out of a read-only child, whose
// parent would otherwise answer for a change it did not read.
func (*settingTool) ReadOnlyExecutionHostMutation() bool { return true }

func (*settingTool) DecisionBarrierCall(args json.RawMessage) bool {
	var in struct {
		Op string `json:"op"`
	}
	return json.Unmarshal(args, &in) == nil && in.Op == "propose"
}

func (t *settingTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Op     string          `json:"op"`
		Query  string          `json:"query"`
		ID     string          `json:"id"`
		Value  json.RawMessage `json:"value"`
		Reason string          `json:"reason"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("setting needs {\"op\": list|propose|open, ...}: %w", err)
	}
	host, ok := t.link.Host()
	if !ok {
		return "", tool.Refusal{Code: "setting.unavailable", Message: "Settings cannot be changed from this session."}
	}
	switch in.Op {
	case "list":
		return listSettings(host.SettingsList(in.Query))
	case "propose":
		if strings.TrimSpace(in.ID) == "" || len(in.Value) == 0 {
			return "", errors.New("setting.invalid_value: op=propose needs an id and a value")
		}
		r, err := host.ProposeSetting(ctx, in.ID, in.Value, in.Reason)
		if err != nil {
			return "", err
		}
		out, _ := json.Marshal(r)
		return string(out), nil
	case "open":
		if err := host.OpenSetting(in.ID); err != nil {
			return "", err
		}
		return fmt.Sprintf("Pointed the user at %s. Only they can change it; carry on without it and say what it blocks.", in.ID), nil
	}
	return "", errors.New("setting needs op list, propose or open")
}

const settingListMax = 40

func listSettings(rows []tool.SettingInfo) (string, error) {
	truncated := len(rows) > settingListMax
	if truncated {
		rows = rows[:settingListMax]
	}
	out, err := json.Marshal(struct {
		Settings  []tool.SettingInfo `json:"settings"`
		Truncated bool               `json:"truncated,omitempty"`
	}{rows, truncated})
	return string(out), err
}
