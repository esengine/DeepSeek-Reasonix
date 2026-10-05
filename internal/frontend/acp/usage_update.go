package acp

// usageUpdate is ACP's usage_update session/update: the context the next
// request carries against the model's window, and the session's cost so far.
type usageUpdate struct {
	SessionUpdate string     `json:"sessionUpdate"`
	Used          int        `json:"used"`
	Size          int        `json:"size"`
	Cost          *usageCost `json:"cost,omitempty"`
}

type usageCost struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

// usageUpdateFor builds the update from the controller's context gauge and the
// session's cumulative usage. used and size are required by the protocol, so a
// session with no window yet reports nothing rather than a zero-sized one.
// Cost rides only when every priced event produced one.
func usageUpdateFor(used, size int, cumulative ReasonixUsage) (usageUpdate, bool) {
	if size <= 0 {
		return usageUpdate{}, false
	}
	u := usageUpdate{SessionUpdate: "usage_update", Used: max(used, 0), Size: size}
	if cumulative.EstimatedCost != nil && cumulative.Currency != nil && *cumulative.Currency != "" {
		u.Cost = &usageCost{Amount: *cumulative.EstimatedCost, Currency: *cumulative.Currency}
	}
	return u, true
}

func (s *service) sendUsageUpdate(sess *acpSession) {
	if sess == nil || sess.sink == nil {
		return
	}
	ctrl := sess.currentCtrl()
	if ctrl == nil {
		return
	}
	used, size := ctrl.ContextSnapshot()
	if u, ok := usageUpdateFor(used, size, sess.statusSnapshot().Usage.Cumulative); ok {
		sess.sink.send(u)
	}
}
