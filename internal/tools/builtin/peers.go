package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"reasonix/internal/contract/tool"
)

// PeerTools lets the model see the other sessions on this machine and send one
// of them text. They exist only in a session that turned messages between
// sessions on, so a session with them off has no such tool in its schema.
func PeerTools(link *tool.PeerLink) []tool.Tool {
	return []tool.Tool{&listPeers{link: link}, &sendPeerMessage{link: link}}
}

type listPeers struct{ link *tool.PeerLink }

func (*listPeers) Name() string { return "list_peers" }
func (*listPeers) Description() string {
	return "List the other Reasonix sessions on this machine that are listening for messages. Use the name or key it prints as the target of send_peer_message."
}
func (*listPeers) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
}
func (*listPeers) ReadOnly() bool { return true }

func (t *listPeers) Execute(_ context.Context, _ json.RawMessage) (string, error) {
	dir, ok := t.link.Directory()
	if !ok {
		return "peer.disabled: messages between sessions are not available in this session", nil
	}
	peers := dir.Peers()
	if len(peers) == 0 {
		return "No other session on this machine is listening.", nil
	}
	var b strings.Builder
	for _, p := range peers {
		name := p.Title
		if p.Project != "" {
			name = p.Project + "/" + p.Title
		}
		fmt.Fprintf(&b, "%s\tkey=%s\n", name, p.Key)
	}
	return b.String(), nil
}

type sendPeerMessage struct{ link *tool.PeerLink }

func (*sendPeerMessage) Name() string { return "send_peer_message" }
func (*sendPeerMessage) Description() string {
	return "Send text to another Reasonix session on this machine, named by list_peers. While the user's dispatch switch is on (the default) its model reads it as soon as that session is free, labelled as coming from you; with the switch off it waits there for its user to approve it. Sending grants no permission and is not a way to run anything there: it is for passing information. Do not include secrets."
}
func (*sendPeerMessage) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"to":{"type":"string","description":"The session's key, or its title, or project/title, as list_peers prints it"},"text":{"type":"string","description":"What to tell that session"}},"required":["to","text"],"additionalProperties":false}`)
}
func (*sendPeerMessage) ReadOnly() bool { return false }

func (t *sendPeerMessage) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		To   string `json:"to"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("send_peer_message needs {\"to\", \"text\"}: %w", err)
	}
	dir, ok := t.link.Directory()
	if !ok {
		return "", errors.New("peer.disabled: messages between sessions are not available in this session")
	}
	receipt, err := dir.SendPeerMessage(in.To, in.Text)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Delivered to %q as %s. Its model reads it as soon as that session is free (it waits for its user only if the dispatch switch is off); there is no reply unless that session sends one.", receipt.To.Title, receipt.ID), nil
}
