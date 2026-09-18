package session

import "reasonix/internal/provider"

const EventImageOffload = "image/offload"

func projectImageOffload(projection *Projection, ev Event) error {
	var body provider.ImageOffloadPayload
	if err := strictPayload(ev.Payload, &body); err != nil {
		return damagedPayload(ev, err)
	}
	projection.ImageOffload = provider.MergeImageOffload(projection.ImageOffload, body.Targets)
	applyAccumulatedImageOffload(projection)
	return nil
}

func applyAccumulatedImageOffload(projection *Projection) {
	if projection == nil || len(projection.ImageOffload) == 0 {
		return
	}
	projection.ModelMessages = provider.ApplyImageOffload(projection.ModelMessages, projection.ImageOffload)
}

func cloneImageOffload(targets []provider.ImageOffloadTarget) []provider.ImageOffloadTarget {
	if len(targets) == 0 {
		return nil
	}
	out := make([]provider.ImageOffloadTarget, len(targets))
	for i, t := range targets {
		out[i] = provider.ImageOffloadTarget{
			MessageID:    t.MessageID,
			ImageIndexes: append([]int(nil), t.ImageIndexes...),
		}
	}
	return out
}
