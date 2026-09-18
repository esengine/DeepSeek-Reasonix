package agent

import "reasonix/internal/provider"

func (a *Agent) SetVisionFilePromoter(fn func([]provider.Message) []provider.Message) {
	if a == nil {
		return
	}
	a.sess.visionFilePromoter = fn
}

func (a *Agent) promoteVisionFiles(msgs []provider.Message) []provider.Message {
	if a == nil || a.sess.visionFilePromoter == nil {
		return msgs
	}
	return a.sess.visionFilePromoter(msgs)
}
