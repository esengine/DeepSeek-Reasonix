package agent

import (
	"context"

	"reasonix/internal/provider"
)

func (a *Agent) SetImageOffloadRecorder(fn func(provider.ImageOffloadPayload)) {
	if a == nil {
		return
	}
	a.sess.imageOffloadMu.Lock()
	a.sess.imageOffloadRecorder = fn
	a.sess.imageOffloadMu.Unlock()
}

func (a *Agent) applyImageOffload(msgs []provider.Message) []provider.Message {
	if a == nil || len(msgs) == 0 {
		return msgs
	}
	a.sess.imageOffloadMu.Lock()
	existing := append([]provider.ImageOffloadTarget(nil), a.sess.imageOffload...)
	a.sess.imageOffloadMu.Unlock()
	return provider.ApplyImageOffload(msgs, existing)
}

func (a *Agent) checkRetainedImages(msgs []provider.Message) error {
	if a == nil || !a.imageInput.native {
		return nil
	}
	return provider.CheckRetainedImages(msgs)
}

func (a *Agent) recordImageOffloadCount(msgs []provider.Message, count int) bool {
	if a == nil || count <= 0 {
		return false
	}
	extra := provider.OffloadOldestImages(msgs, count)
	if len(extra) == 0 {
		return false
	}
	a.sess.imageOffloadMu.Lock()
	a.sess.imageOffload = provider.MergeImageOffload(a.sess.imageOffload, extra)
	rec := a.sess.imageOffloadRecorder
	a.sess.imageOffloadMu.Unlock()
	if rec != nil {
		rec(provider.ImageOffloadPayload{Targets: extra})
	}
	return true
}

func (a *Agent) recoverImageOffload(ctx context.Context, frozen samplingRequest, err error) (samplingRequest, bool) {
	reqErr := provider.AsImageOffloadRequired(err)
	if reqErr == nil || reqErr.OffloadImages <= 0 {
		return samplingRequest{}, false
	}
	if !a.recordImageOffloadCount(frozen.req.Messages, reqErr.OffloadImages) {
		return samplingRequest{}, false
	}
	rebuilt, rerr := a.buildSamplingRequest(ctx, CompactionTriggerPressure)
	if rerr != nil {
		return samplingRequest{}, false
	}
	if aerr := a.applyAdmissionToRequest(&rebuilt.req); aerr != nil {
		return samplingRequest{}, false
	}
	shape := a.requestCalibrationShape(rebuilt.req)
	a.sess.output.activeReqShape.Store(&shape)
	return samplingRequest{req: freezeProviderRequest(rebuilt.req)}, true
}
