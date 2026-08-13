package main

import "reasonix/internal/event"

// topicStatusAwaitingDelivery is a delivery-check pause, not a recovery pause.
const topicStatusAwaitingDelivery = "awaiting_delivery"

func topicStatusFromTurnDone(outcome string) (string, bool) {
	switch outcome {
	case event.TurnOutcomeFinalReadiness:
		return topicStatusAwaitingDelivery, true
	case event.TurnOutcomeRecoveryPaused:
		return topicStatusPaused, true
	default:
		return "", false
	}
}
