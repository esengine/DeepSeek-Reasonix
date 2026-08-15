package plugin

import "encoding/json"

func (t *stdioTransport) handleInboundLine(line []byte, replies chan<- any) {
	probe, ok := decodeInboundMessage(line)
	if !ok {
		return // unparseable line cannot be routed; keep the transport alive
	}
	if probe.Method != "" {
		if isNotificationID(probe.ID) {
			if probe.Method == "notifications/progress" {
				t.progress.dispatchProgress(probe.Params)
			}
			t.notifications.dispatchNotification(probe.Method, probe.Params)
			return
		}
		response := serverRequestReply(probe.ID, probe.Method, t.roots)
		select {
		case replies <- response:
		default:
			// The reply writer is stalled behind a full stdin pipe. An
			// unanswered request degrades to the server's own timeout; a
			// blocked readLoop could deadlock both pipes.
		}
		return
	}

	var response rpcResponse
	if err := json.Unmarshal(line, &response); err != nil {
		return
	}
	t.mu.Lock()
	pending := t.pending[response.ID]
	delete(t.pending, response.ID)
	t.mu.Unlock()
	if pending != nil {
		pending <- response // buffered(1): never blocks after caller cancellation
	}
}
