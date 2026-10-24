package control

import (
	"encoding/json"
	"reasonix/internal/event"
	"reasonix/internal/session"
	"reasonix/internal/transcript"
)

func displayNoticeEvents(e event.Event) ([]session.Event, error) {
	var out []session.Event
	if (e.Code != event.NoticeCodeMCPToolsList && e.Code != event.NoticeCodeImageReplayUnavailable && e.Code != event.NoticeCodeProviderRequestFailed) || e.MessageID == "" {
		return nil, nil
	}
	var buffer transcript.Buffer
	buffer.Apply(e)
	for _, row := range buffer.Messages() {
		data, err := json.Marshal(row)
		if err != nil {
			return nil, err
		}
		var display map[string]any
		if err = json.Unmarshal(data, &display); err != nil {
			return nil, err
		}
		display["id"] = e.MessageID
		payload, err := json.Marshal(map[string]any{"type": "display-notice-v1", "displayRecord": display})
		if err != nil {
			return nil, err
		}
		out = append(out, session.Event{Kind: "diagnostic", Optional: true, Payload: payload})
	}

	return out, nil
}
