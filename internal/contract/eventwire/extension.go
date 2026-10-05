// The JSON wire form of the extension UI surfaces. Kept beside each other
// rather than in wire.go: a new surface kind touches every one of them, and
// the conversion is the only thing in this package that is not a one-liner.
package eventwire

import (
	"encoding/json"

	"reasonix/internal/contract/event"
)

// ExtensionSurface is the JSON form of an event.ExtensionSurfacePayload.
type ExtensionSurface struct {
	PluginID     string                 `json:"pluginId"`
	SurfaceID    string                 `json:"surfaceId"`
	SessionID    string                 `json:"sessionId,omitempty"`
	Generation   uint64                 `json:"generation,omitempty"`
	Kind         string                 `json:"kind"`
	Status       *ExtensionStatus       `json:"status,omitempty"`
	Card         *ExtensionCard         `json:"card,omitempty"`
	Form         *ExtensionForm         `json:"form,omitempty"`
	Notification *ExtensionNotification `json:"notification,omitempty"`
	Panel        *ExtensionPanel        `json:"panel,omitempty"`
	View         *ExtensionView         `json:"view,omitempty"`
}

// ExtensionView is the JSON form of an event.ExtensionViewSurface: a tree the
// frontend renders with its own components.
type ExtensionView struct {
	Slot   string              `json:"slot,omitempty"`
	Anchor string              `json:"anchor,omitempty"`
	Body   []ExtensionViewNode `json:"body"`
}

// ExtensionViewNode is one primitive of a view.
type ExtensionViewNode struct {
	Kind     string              `json:"kind"`
	Value    string              `json:"value,omitempty" externalizable:"true"`
	Key      string              `json:"key,omitempty"`
	Label    string              `json:"label,omitempty"`
	Tone     string              `json:"tone,omitempty"`
	Progress *float64            `json:"progress,omitempty"`
	ActionID string              `json:"actionId,omitempty"`
	Children []ExtensionViewNode `json:"children,omitempty"`
}

// ExtensionPanel is the JSON form of an event.ExtensionPanelView.
type ExtensionPanel struct {
	Title    string               `json:"title,omitempty"`
	Text     string               `json:"text,omitempty" externalizable:"true"`
	Fields   []ExtensionKeyValue  `json:"fields,omitempty"`
	Progress *float64             `json:"progress,omitempty"`
	Actions  []ExtensionActionRef `json:"actions,omitempty"`
}

// ExtensionStatus is the JSON form of an event.ExtensionStatusView.
type ExtensionStatus struct {
	Label    string   `json:"label"`
	Detail   string   `json:"detail,omitempty"`
	Severity string   `json:"severity,omitempty"`
	Progress *float64 `json:"progress,omitempty"`
}

// ExtensionKeyValue is the JSON form of an event.ExtensionKeyValue.
type ExtensionKeyValue struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// ExtensionActionRef is the JSON form of an event.ExtensionActionRef.
type ExtensionActionRef struct {
	ActionID string `json:"actionId"`
	Label    string `json:"label"`
}

// ExtensionCard is the JSON form of an event.ExtensionCardView.
type ExtensionCard struct {
	Title    string               `json:"title,omitempty"`
	Markdown string               `json:"markdown,omitempty" externalizable:"true"`
	Text     string               `json:"text,omitempty" externalizable:"true"`
	Fields   []ExtensionKeyValue  `json:"fields,omitempty"`
	Progress *float64             `json:"progress,omitempty"`
	Actions  []ExtensionActionRef `json:"actions,omitempty"`
}

// ExtensionFormField is the JSON form of an event.ExtensionFormField. Default
// travels as raw JSON: the remote schema has no "any" type, and the field is
// already protocol-validated JSON on arrival.
type ExtensionFormField struct {
	Key      string          `json:"key"`
	Label    string          `json:"label,omitempty"`
	Kind     string          `json:"kind,omitempty"`
	Options  []string        `json:"options,omitempty"`
	Default  json.RawMessage `json:"default,omitempty"`
	Required bool            `json:"required,omitempty"`
}

// ExtensionForm is the JSON form of an event.ExtensionFormView.
type ExtensionForm struct {
	Title   string               `json:"title,omitempty"`
	Message string               `json:"message,omitempty" externalizable:"true"`
	Fields  []ExtensionFormField `json:"fields"`
}

// ExtensionNotification is the JSON form of an event.ExtensionNotificationView.
type ExtensionNotification struct {
	Title    string `json:"title"`
	Body     string `json:"body,omitempty" externalizable:"true"`
	Severity string `json:"severity,omitempty"`
}

// ToWireExtensionSurface converts an event.ExtensionSurfacePayload into its
// JSON wire form. A nil payload yields nil so a malformed event never
// marshals a half-filled extension object.
func ToWireExtensionSurface(p *event.ExtensionSurfacePayload) *ExtensionSurface {
	if p == nil {
		return nil
	}
	out := &ExtensionSurface{
		PluginID: p.PluginID, SurfaceID: p.SurfaceID, SessionID: p.SessionID,
		Generation: p.Generation, Kind: p.Kind,
	}
	if s := p.Status; s != nil {
		out.Status = &ExtensionStatus{Label: s.Label, Detail: s.Detail, Severity: s.Severity, Progress: s.Progress}
	}
	if c := p.Card; c != nil {
		card := &ExtensionCard{
			Title: c.Title, Markdown: c.Markdown, Text: c.Text, Progress: c.Progress,
		}
		if len(c.Fields) > 0 {
			card.Fields = make([]ExtensionKeyValue, len(c.Fields))
			for i, f := range c.Fields {
				card.Fields[i] = ExtensionKeyValue{Key: f.Key, Value: f.Value}
			}
		}
		if len(c.Actions) > 0 {
			card.Actions = make([]ExtensionActionRef, len(c.Actions))
			for i, a := range c.Actions {
				card.Actions[i] = ExtensionActionRef{ActionID: a.ActionID, Label: a.Label}
			}
		}
		out.Card = card
	}
	if f := p.Form; f != nil {
		form := &ExtensionForm{Title: f.Title, Message: f.Message}
		if len(f.Fields) > 0 {
			form.Fields = make([]ExtensionFormField, len(f.Fields))
			for i, field := range f.Fields {
				wireField := ExtensionFormField{
					Key: field.Key, Label: field.Label, Kind: field.Kind,
					Options:  append([]string(nil), field.Options...),
					Required: field.Required,
				}
				if field.Default != nil {
					// The value arrived as protocol-validated JSON, so a
					// marshal failure here is unreachable in practice; a
					// pathological in-memory value simply drops the default.
					if raw, err := json.Marshal(field.Default); err == nil {
						wireField.Default = raw
					}
				}
				form.Fields[i] = wireField
			}
		}
		out.Form = form
	}
	if n := p.Notification; n != nil {
		out.Notification = &ExtensionNotification{Title: n.Title, Body: n.Body, Severity: n.Severity}
	}
	if pv := p.Panel; pv != nil {
		panel := &ExtensionPanel{Title: pv.Title, Text: pv.Text, Progress: pv.Progress}
		for _, f := range pv.Fields {
			panel.Fields = append(panel.Fields, ExtensionKeyValue{Key: f.Key, Value: f.Value})
		}
		for _, a := range pv.Actions {
			panel.Actions = append(panel.Actions, ExtensionActionRef{ActionID: a.ActionID, Label: a.Label})
		}
		out.Panel = panel
	}
	if v := p.View; v != nil {
		out.View = &ExtensionView{Slot: v.Slot, Anchor: v.Anchor, Body: viewNodes(v.Body)}
	}
	return out
}

func viewNodes(in []event.ExtensionViewNode) []ExtensionViewNode {
	if len(in) == 0 {
		return nil
	}
	out := make([]ExtensionViewNode, 0, len(in))
	for _, n := range in {
		out = append(out, ExtensionViewNode{
			Kind: n.Kind, Value: n.Value, Key: n.Key, Label: n.Label,
			Tone: n.Tone, Progress: n.Progress, ActionID: n.ActionID,
			Children: viewNodes(n.Children),
		})
	}
	return out
}
