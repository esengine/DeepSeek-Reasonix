package session

import (
	"context"
	"encoding/json"
	"reasonix/internal/sessioncontent"
)

func indexTurnEvent(ctx context.Context, content *sessioncontent.Store, state *historyBuildState, event Event) error {
	if state.commitTurn != "" && (event.Kind == "assistant/attempt" || event.Kind == "tool/call") {
		payload := event.Payload
		if event.PayloadRef != nil {
			var err error
			payload, err = resolveContentPayload(ctx, content, *event.PayloadRef)
			if err != nil {
				return err
			}
		}
		var body struct {
			ID     string `json:"id"`
			Action string `json:"action"`
		}
		if err := json.Unmarshal(payload, &body); err != nil {
			return err
		}
		if body.ID != "" && (event.Kind == "tool/call" || body.Action == "begin") {
			_, err := state.tx.ExecContext(ctx, `INSERT OR IGNORE INTO turn_counts(turn_id,kind,id,sequence) VALUES(?,?,?,?)`, state.commitTurn, event.Kind, body.ID, event.Sequence)
			return err
		}
	}
	if state.commitTurn != "" {
		switch event.Kind {
		case "turn/start":
			_, err := state.tx.ExecContext(ctx, `INSERT INTO turn_summaries(turn_id,start_sequence,started_at) VALUES(?,?,?) ON CONFLICT(turn_id) DO UPDATE SET start_sequence=excluded.start_sequence,started_at=excluded.started_at`, state.commitTurn, event.Sequence, state.commitTime)
			return err
		case "turn/end":
			_, err := state.tx.ExecContext(ctx, `UPDATE turn_summaries SET end_sequence=?,ended_at=MAX(ended_at,?) WHERE turn_id=?`, event.Sequence, state.commitTime, state.commitTurn)
			return err
		}
	}
	return nil
}
