package schedule

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// MinAtLead is how far ahead a one-shot trigger must sit when created.
const MinAtLead = time.Minute

// TriggerID is the dedup key of one slot: the schedule and the slot's Unix second.
func TriggerID(scheduleID string, slot time.Time) string {
	return scheduleID + "/" + strconv.FormatInt(slot.Unix(), 10)
}

// IsSlot reports whether t is exactly a slot of the trigger. Slots of an
// interval trigger are anchor + k*every for k >= 1, so they never drift.
func (t Trigger) IsSlot(slot time.Time) bool {
	switch t.Kind {
	case TriggerAt:
		return slot.Unix() == t.At.Unix()
	case TriggerEvery:
		every := t.EverySec
		d := slot.Unix() - t.Anchor.Unix()
		return every > 0 && d >= every && d%every == 0
	}
	return false
}

// LatestSlot is the most recent slot at or before now. Only the latest is
// returned: a long absence yields one slot, not a backlog.
func (t Trigger) LatestSlot(now time.Time) (time.Time, bool) {
	switch t.Kind {
	case TriggerAt:
		if now.Before(t.At) {
			return time.Time{}, false
		}
		return t.At.UTC(), true
	case TriggerEvery:
		if t.EverySec <= 0 {
			return time.Time{}, false
		}
		d := now.Unix() - t.Anchor.Unix()
		if d < t.EverySec {
			return time.Time{}, false
		}
		return time.Unix(t.Anchor.Unix()+d/t.EverySec*t.EverySec, 0).UTC(), true
	}
	return time.Time{}, false
}

func (t Trigger) validate(p Policy, now time.Time) error {
	switch t.Kind {
	case TriggerAt:
		if t.At.Before(now.Add(MinAtLead)) {
			return fmt.Errorf("%w: at must be at least %s ahead", ErrInvalid, MinAtLead)
		}
	case TriggerEvery:
		if time.Duration(t.EverySec)*time.Second < p.MinInterval() {
			return fmt.Errorf("%w: every %ds, minimum %s", ErrIntervalBelowFloor, t.EverySec, p.MinInterval())
		}
	default:
		return fmt.Errorf("%w: unknown trigger kind %q", ErrInvalid, t.Kind)
	}
	return nil
}

// Digest is the sha256 over everything a person confirmed. Expiry and status
// are not in it: renewing or pausing is not a change of what was agreed to.
func Digest(s Schedule) string {
	canon := struct {
		Trigger Trigger `json:"trigger"`
		Target  Target  `json:"target"`
		Prompt  string  `json:"prompt"`
		Model   Model   `json:"model"`
		Grant   Grant   `json:"grant"`
		Budget  Budget  `json:"budget"`
	}{s.Trigger, s.Target, s.Prompt, s.Model, s.Grant, s.Budget}
	b, _ := json.Marshal(canon)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
