package main

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"reasonix/internal/session"
	"reasonix/internal/store"
)

type sessionLocatorKind uint8

const (
	sessionLocatorEmpty sessionLocatorKind = iota
	sessionLocatorCanonical
	sessionLocatorLegacy
	sessionLocatorInvalid
)

type legacySessionPath string

type sessionLocator struct {
	kind       sessionLocatorKind
	ref        session.SessionRef
	legacyPath legacySessionPath
	reason     string
}

func isLegacySessionTranscriptName(name string) bool {
	if runtime.GOOS == "windows" {
		name = strings.ToLower(name)
	}
	return store.IsSessionTranscriptName(name)
}

func validatedLegacySessionPathForRead(raw string) (legacySessionPath, bool) {
	locator := classifySessionLocator(raw)
	if locator.kind != sessionLocatorLegacy {
		return "", false
	}
	path := string(locator.legacyPath)
	if !filepath.IsAbs(path) {
		return "", false
	}
	validated, err := resolveLegacySessionPath(path, filepath.Dir(path))
	return validated, err == nil
}

func legacySessionPathForFileAccess(raw string) (legacySessionPath, bool, error) {
	locator := classifySessionLocator(raw)
	switch locator.kind {
	case sessionLocatorEmpty, sessionLocatorCanonical:
		return "", false, nil
	case sessionLocatorInvalid:
		return "", false, &sessionLocatorError{reason: locator.reason}
	}
	path := string(locator.legacyPath)
	if !filepath.IsAbs(path) {
		return "", false, &sessionLocatorError{reason: "legacy_path_not_absolute"}
	}
	validated, err := resolveLegacySessionPath(path, filepath.Dir(path))
	if err != nil {
		return "", false, err
	}
	return validated, true, nil
}

type sessionLocatorError struct {
	reason string
}

func (e *sessionLocatorError) Error() string {
	if e == nil || e.reason == "" {
		return "invalid session identity"
	}
	return fmt.Sprintf("invalid session identity: %s", e.reason)
}

// classifySessionLocator is deliberately path-I/O free. A value with the
// canonical route prefix is always a route candidate: malformed IDs never
// fall through to the legacy path branch.
func classifySessionLocator(raw string) sessionLocator {
	value := strings.TrimSpace(raw)
	if value == "" {
		return sessionLocator{kind: sessionLocatorEmpty}
	}
	if id, route := strings.CutPrefix(value, remoteSessionIDRoutePrefix); route {
		id = strings.TrimSpace(id)
		if err := session.ValidateSessionID(id); err != nil {
			return sessionLocator{kind: sessionLocatorInvalid, reason: "invalid_canonical_route"}
		}
		return sessionLocator{
			kind: sessionLocatorCanonical,
			ref:  session.SessionRef{HostID: localDesktopHostID, SessionID: id},
		}
	}
	return sessionLocator{kind: sessionLocatorLegacy, legacyPath: legacySessionPath(value)}
}

func resolveLegacySessionPath(raw string, dirs ...string) (legacySessionPath, error) {
	locator := classifySessionLocator(raw)
	switch locator.kind {
	case sessionLocatorEmpty:
		return "", &sessionLocatorError{reason: "empty"}
	case sessionLocatorCanonical:
		return "", &sessionLocatorError{reason: "canonical_route_is_not_a_path"}
	case sessionLocatorInvalid:
		return "", &sessionLocatorError{reason: locator.reason}
	}
	for _, dir := range dirs {
		if path, ok := pinnedTabSessionPath(dir, string(locator.legacyPath)); ok {
			return legacySessionPath(path), nil
		}
	}
	return "", &sessionLocatorError{reason: "invalid_legacy_path"}
}
