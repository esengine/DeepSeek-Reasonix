package session

import (
	"fmt"
	"strings"

	"reasonix/internal/provider"
)

func projectImageIsolation(projection *Projection, _ Commit, ev Event) error {
	var decision provider.ImageIsolationDecision
	if err := strictPayload(ev.Payload, &decision); err != nil {
		return damagedPayload(ev, err)
	}
	if err := validateImageIsolation(decision); err != nil {
		return damagedPayload(ev, err)
	}
	key := provider.ImageDecisionKey(decision)
	projection.ImageIsolations[key] = decision
	refreshModelImageIsolations(projection)
	return nil
}

func validateImageIsolation(decision provider.ImageIsolationDecision) error {
	if decision.Version != 1 {
		return fmt.Errorf("unsupported image isolation version %d", decision.Version)
	}
	identity := decision.Identity
	if strings.TrimSpace(identity.MessageID) == "" || identity.ImageOrdinal < 0 || len(identity.ContentDigest) != 64 {
		return fmt.Errorf("invalid image identity")
	}
	for _, r := range identity.ContentDigest {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return fmt.Errorf("invalid image digest")
		}
	}
	switch decision.Scope.Kind {
	case "content":
	case "provider":
		if strings.TrimSpace(decision.Scope.Provider) == "" && strings.TrimSpace(decision.Scope.Model) == "" {
			return fmt.Errorf("provider image isolation scope is empty")
		}
	default:
		return fmt.Errorf("invalid image isolation scope %q", decision.Scope.Kind)
	}
	if strings.TrimSpace(decision.Reason) == "" {
		return fmt.Errorf("image isolation reason is required")
	}
	return nil
}

func refreshModelImageIsolations(projection *Projection) {
	if projection == nil || len(projection.ModelMessages) == 0 {
		return
	}
	for i := range projection.ModelMessages {
		projection.ModelMessages[i].ImageIsolations = nil
	}
	for _, decision := range projection.ImageIsolations {
		index := projectionMessageIndex(projection.ModelMessages, decision.Identity.MessageID)
		if index < 0 {
			continue
		}
		message := &projection.ModelMessages[index]
		ordinal := decision.Identity.ImageOrdinal
		if ordinal >= len(message.Images) || provider.ImageContentDigest(message.Images[ordinal]) != decision.Identity.ContentDigest {
			continue
		}
		message.ImageIsolations = append(message.ImageIsolations, decision)
	}
}
