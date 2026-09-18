package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/attachment"
	"reasonix/internal/provider"
	"reasonix/internal/sessioncontent"
)

func indexOneMessage(ctx context.Context, content *sessioncontent.Store, state *historyBuildState, message provider.Message, sequence uint64, upsert bool) error {
	body, err := json.Marshal(message)
	if err != nil {
		return err
	}
	return indexOneMessageBody(ctx, content, state, message, sequence, upsert, body)
}

func indexOneMessageBody(ctx context.Context, content *sessioncontent.Store, state *historyBuildState, message provider.Message, sequence uint64, upsert bool, body json.RawMessage) error {
	id := strings.TrimSpace(message.ID)
	if id == "" {
		return errors.New("session: indexed message has no stable id")
	}
	position, exists := state.positions[id]
	visibleTurn := state.turns[id]
	if !exists {
		state.nextPosition++
		position = state.nextPosition
		state.positions[id] = position
		if agent.IsUserAuthoredTurnMessage(message) {
			state.visibleTurn++
		}
		visibleTurn = state.visibleTurn
		state.turns[id] = visibleTurn
	} else if !upsert {
		return fmt.Errorf("session: duplicate indexed message id %q", id)
	}
	version := state.versions[id] + 1
	state.versions[id] = version
	if exists {
		if err := flushHistoryBuildRows(ctx, state.tx, state); err != nil {
			return err
		}
		if _, err := state.statements.expire.ExecContext(ctx, sequence, id); err != nil {
			return err
		}
	}
	ref, err := content.Put(ctx, bytes.NewReader(body), sessioncontent.Metadata{MediaType: "application/json"})
	if err != nil {
		return err
	}
	if err := insertContentRef(ctx, state, ref); err != nil {
		return err
	}
	for _, extra := range attachment.CollectContentRefs(message.ImageInputs) {
		if err := insertContentRef(ctx, state, extra); err != nil {
			return err
		}
	}
	if message.Role == provider.RoleTool && message.ToolCallID != "" {
		if _, err := state.tx.ExecContext(ctx, `INSERT OR IGNORE INTO tool_links(message_id,digest,call_id,is_result,state) VALUES(?,?,?,1,?)`, id, ref.Digest, message.ToolCallID, string(provider.ToolResultRunState(message))); err != nil {
			return err
		}
	}
	for _, call := range message.ToolCalls {
		if call.ID != "" {
			if _, err := state.tx.ExecContext(ctx, `INSERT OR IGNORE INTO tool_links(message_id,digest,call_id,is_result,state) VALUES(?,?,?,0,'unknown')`, id, ref.Digest, call.ID); err != nil {
				return err
			}
		}
	}
	visibleUser := 0
	if agent.IsUserAuthoredTurnMessage(message) {
		visibleUser = 1
	}
	state.messages = append(state.messages, []any{id, version, position, sequence, 0, string(message.Role), messagePreview(message), nil, ref.Digest, ref.Bytes, ref.IndexDigest, 1, "", visibleTurn, visibleUser})
	return nil
}

// Re-number only rows whose visible user boundary changed. New versions keep
// previous numbering available to fixed-snapshot cursors.
