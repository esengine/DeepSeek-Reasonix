package cli

import (
	"context"
	"fmt"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/session"
)

// The CLI resume surface reads only the canonical sessions-v4 store. Legacy
// transcripts are converted once at startup (see legacy_session_migration.go);
// they are never a resume target.

// v4ResumeLocatorPrefix marks a synthetic resume locator that names a native
// sessions-v4 identity. Resume rows carry it in SessionInfo.Path so the
// existing listing, grouping, completion, and picker surfaces stay
// path-shaped over v4 identities.
const v4ResumeLocatorPrefix = "session-v4:"

func v4ResumeLocator(sessionID string) string {
	return v4ResumeLocatorPrefix + sessionID
}

// v4ResumeID returns the native v4 session id encoded in a resume locator.
func v4ResumeID(locator string) (string, bool) {
	id, ok := strings.CutPrefix(strings.TrimSpace(locator), v4ResumeLocatorPrefix)
	if !ok || strings.TrimSpace(id) == "" {
		return "", false
	}
	return id, true
}

// isNativeResume reports whether a resume target names a v4 identity.
func isNativeResume(locator string) bool {
	_, ok := v4ResumeID(locator)
	return ok
}

// resumeTargetDisplay renders a resume target for user-facing output: the bare
// session id for a native locator, otherwise the target unchanged.
func resumeTargetDisplay(locator string) string {
	if id, ok := v4ResumeID(locator); ok {
		return id
	}
	return locator
}

// nativeResumeService is the v4 host service for a CLI session directory. The
// legacy directory's sibling sessions-v4 root is the canonical store.
func nativeResumeService(dir string) *session.Service {
	return cliSessionService(dir)
}

// resumeRows lists the resumable native v4 sessions for a CLI session
// directory as rows carrying a synthetic locator.
func resumeRows(dir string) []agent.SessionInfo {
	service := nativeResumeService(dir)
	if dir == "" || service == nil || service.Query() == nil {
		return nil
	}
	ctx := context.Background()
	var rows []agent.SessionInfo
	cursor := ""
	for {
		page, err := service.Query().List(ctx, cursor, 100)
		if err != nil {
			break
		}
		for i := range page.Sessions {
			info := page.Sessions[i]
			if !isCanonicalSessionID(info.SessionID) {
				// A managed legacy session-events mirror is keyed by its legacy
				// branch name, not a canonical hash identity. It is not an
				// independent native conversation.
				continue
			}
			if info.MetadataStatus == session.MetadataReady && info.Turns == 0 {
				// Never had a user turn — an empty conversation that must not
				// appear in the history panel or the resume picker.
				continue
			}
			rows = append(rows, nativeResumeRow(info))
		}
		if page.NextCursor == "" || page.NextCursor == cursor {
			break
		}
		cursor = page.NextCursor
	}
	return rows
}

func nativeResumeRow(info session.SessionInfo) agent.SessionInfo {
	preview := info.Preview
	if info.MetadataStatus != session.MetadataReady {
		// The catalog cache is rebuilding; keep the row identifiable instead of
		// blank, matching the legacy listing's unindexed-session placeholder.
		preview = "History is being indexed — " + info.SessionID
	}
	return agent.SessionInfo{
		Path:           v4ResumeLocator(info.SessionID),
		CreatedAt:      info.CreatedAt,
		LastActivityAt: info.UpdatedAt,
		ModTime:        info.UpdatedAt,
		Preview:        preview,
		Turns:          info.Turns,
		CountsKnown:    info.MetadataStatus == session.MetadataReady,
		CustomTitle:    info.Title,
	}
}

// isCanonicalSessionID reports whether id is a hash identity minted by the v4
// store (24-hex migration target or 32-hex random id) rather than a legacy
// branch name reused by a managed session-events mirror.
func isCanonicalSessionID(id string) bool {
	if len(id) != 24 && len(id) != 32 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// resumeRowKey is the stable grouping key for a resume row. It keys on the
// locator so a session id containing a dot is never mistaken for a file
// extension.
func resumeRowKey(s agent.SessionInfo) string {
	if isNativeResume(s.Path) {
		return s.Path
	}
	return agent.BranchID(s.Path)
}

// nativeResumeModelSelection reads the model recorded on a native v4 session so
// a resume can restore the exact selection.
func nativeResumeModelSelection(dir, id string) (string, string, bool) {
	service := nativeResumeService(dir)
	if service == nil || service.Query() == nil {
		return "", "", false
	}
	info, err := service.Query().Stat(context.Background(), session.SessionRef{HostID: service.HostID(), SessionID: id})
	if err != nil {
		return "", "", false
	}
	if strings.TrimSpace(info.ModelRef) == "" {
		return "", "", false
	}
	return info.ModelRef, info.ModelIdentity, true
}

// resumeTargetActive reports whether locator names the controller's current
// session identity.
func (m *chatTUI) resumeTargetActive(locator string) bool {
	id, native := v4ResumeID(locator)
	if !native {
		return false
	}
	if identity, ok := m.ctrl.(control.IdentityLifecycle); ok {
		if ref, ok := identity.SessionRef(); ok {
			return ref.SessionID == id
		}
	}
	return false
}

// resumeIntoController attaches a native v4 target to the running controller.
// Callers snapshot the outgoing session first, matching /resume.
func (m *chatTUI) resumeIntoController(locator string) error {
	id, native := v4ResumeID(locator)
	if !native {
		return fmt.Errorf("unsupported resume target %q", locator)
	}
	identity, ok := m.ctrl.(control.IdentityLifecycle)
	if !ok || !identity.UsesExclusiveSession() {
		return fmt.Errorf("session identity protocol is unavailable")
	}
	service := identity.SessionService()
	if service == nil {
		return fmt.Errorf("session service is unavailable")
	}
	ref := session.SessionRef{HostID: service.HostID(), SessionID: id}
	if _, err := identity.OpenSession(context.Background(), ref); err != nil {
		return err
	}
	// The v4 writer lease lives in the session service, so the legacy
	// single-writer keeper is released rather than moved.
	if m.leases != nil {
		if err := m.leases.Rebind(""); err != nil {
			return err
		}
	}
	return bindChatTUIAuthority(m)
}
