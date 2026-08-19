package control

import (
	"fmt"
	"strings"

	"reasonix/internal/taskcontract"
)

func errUnknownQualityFloor(raw string) error {
	return fmt.Errorf("unknown quality floor %q (accepted: standard, delivery; legacy light folds to standard)", raw)
}

// Session quality floor. Records intent only: enforcement lives in the fact
// contract, which reads the floor stamped on each write receipt. Standard is
// the zero value; legacy light vocabulary folds to standard at parse time.

const (
	QualityFloorStandard = "standard"
	QualityFloorDelivery = "delivery"
)

// NormalizeQualityFloor maps free-form and legacy mode labels onto the two
// floor values. Light and its aliases fold to standard silently; unknown
// values are an error so no new vocabulary can appear.
func NormalizeQualityFloor(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", QualityFloorStandard, "balanced", "full", "normal":
		return QualityFloorStandard, nil
	case QualityFloorDelivery, "deliver", "quality":
		return QualityFloorDelivery, nil
	case "light", "economy", "eco", "lite", "save", "saving", "low", "minimal":
		return QualityFloorStandard, nil
	default:
		return "", errUnknownQualityFloor(raw)
	}
}

func QualityFloorConstraint(floor string) taskcontract.PolicyFloor {
	if strings.TrimSpace(floor) == QualityFloorDelivery {
		return taskcontract.PolicyFloorDelivery
	}
	return taskcontract.PolicyFloorNone
}

// QualityFloor returns the session floor's wire label.
func (c *Controller) QualityFloor() string {
	if c == nil {
		return QualityFloorStandard
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sessionSettings.qualityFloor == "" {
		return QualityFloorStandard
	}
	return c.sessionSettings.qualityFloor
}

// SetQualityFloor updates the session quality floor for subsequent turns. It
// never rebuilds the controller or touches the provider-visible prefix; prior
// obligations keep their receipt stamps, so a downgrade drops only future
// writes' floor treatment.
func (c *Controller) SetQualityFloor(floor string) error {
	normalized, err := NormalizeQualityFloor(floor)
	if err != nil {
		return err
	}
	if c == nil {
		return nil
	}
	c.mu.Lock()
	c.sessionSettings.qualityFloor = normalized
	c.mu.Unlock()
	return nil
}

func (c *Controller) qualityFloorConstraint() taskcontract.PolicyFloor {
	c.mu.Lock()
	floor := c.sessionSettings.qualityFloor
	c.mu.Unlock()
	return QualityFloorConstraint(floor)
}

// sessionSettings groups the per-session posture knobs (plan mode, quality
// floor) that share one lifetime under c.mu.
type sessionSettings struct {
	planMode     bool
	qualityFloor string
}
