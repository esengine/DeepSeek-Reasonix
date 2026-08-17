package agent

import (
	"strings"
	"time"
)

// claimRecoveryModeAt returns the request shape for a new conversation. Before
// NextProbeAt every caller stays in fallback; once due, one process claims the
// half-open lease while peers remain on the stable fallback route.
func (s *missingReasoningWarnState) claimRecoveryModeAt(fingerprint string, observedAt time.Time) missingReasoningRecoveryDecision {
	fingerprint = strings.TrimSpace(fingerprint)
	if s == nil || s.dir == "" || !validMissingReasoningFingerprint(fingerprint) {
		return missingReasoningRecoveryDecision{Mode: missingReasoningRecoveryNormal}
	}
	observedAt = normalizeMissingReasoningObservedAt(observedAt)
	processLock := s.processLock()
	processLock.Lock()
	defer processLock.Unlock()

	release, err := s.acquire()
	if err != nil {
		return missingReasoningRecoveryDecision{Mode: missingReasoningRecoveryNormal}
	}
	defer release()
	incidents, err := s.load(missingReasoningTransactionNow(observedAt))
	if err != nil {
		return missingReasoningRecoveryDecision{Mode: missingReasoningRecoveryNormal}
	}
	incident, exists := incidents[fingerprint]
	if !exists || incident.LastMissingUnixNano <= incident.LastResolvedAtUnixNano || incident.FallbackAtUnixNano == 0 {
		return missingReasoningRecoveryDecision{Mode: missingReasoningRecoveryNormal}
	}
	observedAtUnixNano := observedAt.UnixNano()
	if observedAtUnixNano < incident.NextProbeAtUnixNano {
		return missingReasoningRecoveryDecision{Mode: missingReasoningRecoveryFallback}
	}
	if claimedAt := incident.ProbeClaimedAtUnixNano; claimedAt != 0 &&
		observedAt.Before(time.Unix(0, claimedAt).Add(missingReasoningFallbackProbeLease)) {
		return missingReasoningRecoveryDecision{Mode: missingReasoningRecoveryFallback}
	}
	incident.ProbeClaimedAtUnixNano = observedAtUnixNano
	incidents[fingerprint] = incident
	if s.save(incidents) != nil {
		// A durable claim is required before spending a paid normal probe.
		return missingReasoningRecoveryDecision{Mode: missingReasoningRecoveryFallback}
	}
	return missingReasoningRecoveryDecision{Mode: missingReasoningRecoveryProbe, ProbeClaimedAt: observedAt}
}

// failProbeAt reopens a circuit only for the current half-open owner. The token
// prevents a slow failure from overwriting a newer lease or healthy resolution.
func (s *missingReasoningWarnState) failProbeAt(fingerprint string, probeClaimedAt, observedAt time.Time) bool {
	fingerprint = strings.TrimSpace(fingerprint)
	if s == nil || s.dir == "" || !validMissingReasoningFingerprint(fingerprint) || probeClaimedAt.IsZero() {
		return false
	}
	probeClaimedAt = normalizeMissingReasoningObservedAt(probeClaimedAt)
	observedAt = normalizeMissingReasoningObservedAt(observedAt)
	processLock := s.processLock()
	processLock.Lock()
	defer processLock.Unlock()

	release, err := s.acquire()
	if err != nil {
		return false
	}
	defer release()
	incidents, err := s.load(missingReasoningTransactionNow(observedAt))
	if err != nil {
		return false
	}
	incident, exists := incidents[fingerprint]
	if !exists || incident.FallbackAtUnixNano == 0 ||
		incident.ProbeClaimedAtUnixNano != probeClaimedAt.UnixNano() ||
		observedAt.UnixNano() <= incident.LastResolvedAtUnixNano {
		return false
	}
	observedAtUnixNano := observedAt.UnixNano()
	incident.FallbackLevel = min(max(incident.FallbackLevel, 1)+1, len(missingReasoningFallbackBackoffs))
	incident.FallbackAtUnixNano = observedAtUnixNano
	incident.LastMissingUnixMs = observedAt.UnixMilli()
	incident.LastMissingUnixNano = observedAtUnixNano
	incident.ResolveStreak = 0
	incident.LastHealthyAtUnixNano = 0
	incident.NextProbeAtUnixNano = observedAt.Add(missingReasoningFallbackBackoff(incident.FallbackLevel)).UnixNano()
	incident.ProbeClaimedAtUnixNano = 0
	incidents[fingerprint] = incident
	return s.save(incidents) == nil
}

// resolveProbeAt records health only for the current half-open owner. Healthy
// tool rounds renew the lease without a heartbeat until the circuit closes.
func (s *missingReasoningWarnState) resolveProbeAt(fingerprint string, probeClaimedAt, observedAt time.Time) missingReasoningResolveResult {
	fingerprint = strings.TrimSpace(fingerprint)
	if s == nil || s.dir == "" || !validMissingReasoningFingerprint(fingerprint) || probeClaimedAt.IsZero() {
		return missingReasoningResolveResult{}
	}
	probeClaimedAt = normalizeMissingReasoningObservedAt(probeClaimedAt)
	observedAt = normalizeMissingReasoningObservedAt(observedAt)
	processLock := s.processLock()
	processLock.Lock()
	defer processLock.Unlock()

	release, err := s.acquire()
	if err != nil {
		return missingReasoningResolveResult{}
	}
	defer release()
	incidents, err := s.load(missingReasoningTransactionNow(observedAt))
	if err != nil {
		return missingReasoningResolveResult{}
	}
	incident, exists := incidents[fingerprint]
	if !exists || incident.FallbackAtUnixNano == 0 ||
		incident.ProbeClaimedAtUnixNano != probeClaimedAt.UnixNano() {
		return missingReasoningResolveResult{Recorded: true, Resolved: !exists || incident.LastMissingUnixNano <= incident.LastResolvedAtUnixNano}
	}
	observedAtUnixNano := observedAt.UnixNano()
	if incident.LastMissingUnixNano >= observedAtUnixNano || incident.LastHealthyAtUnixNano >= observedAtUnixNano {
		return missingReasoningResolveResult{Recorded: true, ProbeClaimedAt: probeClaimedAt}
	}
	incident.ResolveStreak++
	incident.LastHealthyAtUnixNano = observedAtUnixNano
	incident.ProbeClaimedAtUnixNano = observedAtUnixNano
	resolved := incident.ResolveStreak >= missingReasoningHealthyResolveStreak
	if resolved {
		incident.ResolveStreak = 0
		incident.LastResolvedAtUnixNano = observedAtUnixNano
		incident.FallbackAtUnixNano = 0
		incident.FallbackLevel = 0
		incident.NextProbeAtUnixNano = 0
		incident.ProbeClaimedAtUnixNano = 0
	}
	incidents[fingerprint] = incident
	if s.save(incidents) != nil {
		return missingReasoningResolveResult{}
	}
	result := missingReasoningResolveResult{Recorded: true, Resolved: resolved}
	if !resolved {
		result.ProbeClaimedAt = observedAt
	}
	return result
}
