package serve

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"slices"
	"strings"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/platform/worktree"
	"reasonix/internal/state/migration"
	"reasonix/internal/state/store"
)

// treeSessionsPerWorkspace bounds one workspace's branch of the tree. A long
// history is paged by opening the workspace, not by shipping all of it to a
// sidebar that shows a dozen rows.
const treeSessionsPerWorkspace = 50

// maxRecoveryLineageWalk bounds the parent walk that groups recovery copies.
// The chain is short by construction; the bound is there so damaged metadata
// cannot spin the sidebar.
const maxRecoveryLineageWalk = 16

// treeWorkspace is one folder in the sidebar: what it is called, whether a pane
// is driving it, and the conversations saved under it.
type treeWorkspace struct {
	Root string `json:"root"`
	Name string `json:"name"`
	// Isolated marks a delivery worktree. Its folder name is the project's, so
	// without this the sidebar shows two rows with one name.
	Isolated bool `json:"isolated,omitempty"`
	Missing  bool `json:"missing,omitempty"`
	Open     bool `json:"open,omitempty"`
	// Remembered marks a folder the user chose. A window always has a root — the
	// one it happened to launch in — and a client that cannot tell the two apart
	// shows "never picked a project" as if it were a project.
	Remembered bool          `json:"remembered,omitempty"`
	Sessions   []treeSession `json:"sessions"`
}

// treeSession is one conversation. RuntimeID is set when a pane already has it
// open, which is what lets the sidebar focus that pane instead of opening a
// second writer for the same transcript.
type treeSession struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	Title     string `json:"title,omitempty"`
	Turns     int    `json:"turns,omitempty"`
	RuntimeID string `json:"runtimeId,omitempty"`
	Archived  bool   `json:"archived,omitempty"`
	// Unread is a turn that finished since the person last looked. The kernel
	// derives it from two stored timestamps; absence reads as seen.
	Unread bool `json:"unread,omitempty"`
	// Copies are this conversation's conflict-recovery copies. A save that
	// keeps conflicting writes one file per turn, all under the one title, and
	// unfolded that is a sidebar of rows the user never made.
	Copies []treeSession `json:"copies,omitempty"`
	// Versions are this conversation as it stood before each rewind cut it,
	// newest first. Each is a whole conversation and opens like one.
	Versions []treeSession `json:"versions,omitempty"`
}

func (h *Hub) registerTreeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /tree", h.tree)
	mux.HandleFunc("POST /tree/workspaces", h.addWorkspace)
	mux.HandleFunc("POST /tree/workspaces/remove", h.removeWorkspace)
	mux.HandleFunc("POST /tree/workspaces/move", h.moveWorkspace)
	mux.HandleFunc("POST /tree/sessions/remove", h.removeSession)
	mux.HandleFunc("POST /tree/sessions/archive", h.archiveSession)
	mux.HandleFunc("POST /tree/sessions/rename", h.renameSession)
	mux.HandleFunc("POST /tree/sessions/auto-name", h.autoNameSession)
	mux.HandleFunc("POST /tree/sessions/export", h.exportSession)
	mux.HandleFunc("POST /tree/sessions/import-legacy", h.importLegacySessions)
}

// tree answers the whole sidebar in one request: every remembered workspace
// with the sessions saved under it. Titles come from each project's cache and
// are never generated here — the pane that opens a session does that.
func (h *Hub) tree(w http.ResponseWriter, _ *http.Request) {
	open := h.openSessions()
	roots := h.roots()
	out := make([]treeWorkspace, 0, len(roots))
	for _, ref := range roots {
		// Open means a pane is driving this folder — true from the moment one is
		// opened, before its first turn has written a transcript to list.
		node := treeWorkspace{
			Root: ref.dir, Name: fileutil.RootName(ref.dir), Open: h.rootPanes(ref.dir) > 0,
			Isolated:   worktree.IsManagedPath(ref.dir, config.DeliveryWorktreeDir()),
			Remembered: ref.remembered,
			Sessions:   []treeSession{},
		}
		if st, err := os.Stat(ref.dir); err != nil || !st.IsDir() {
			node.Missing = true
		}
		node.Sessions = append(node.Sessions, h.workspaceSessions(ref.dir, open)...)
		out = append(out, node)
	}
	writeJSON(w, out)
}

// workspaceSessions lists one folder's conversations, hiding what the pickers
// hide: subagent traces and redundant recovery copies. A copy that a pane has
// open stays, because something is driving it.
func (h *Hub) workspaceSessions(root string, open map[string]string) []treeSession {
	dir := SessionDirFor(root)
	listed, err := sessionstore.ListSessions(dir)
	if err != nil {
		return nil
	}
	titles := h.titleCacheFor(dir).snapshot()
	byID := make(map[string]sessionstore.SessionInfo, len(listed))
	for _, si := range listed {
		byID[sessionstore.BranchID(si.Path)] = si
	}
	out := make([]treeSession, 0, len(listed))
	// Lineage root -> the row that copies of it fold into. The list is
	// newest-first, so the first copy to arrive is the one still being written.
	lead := map[string]int{}
	for _, si := range listed {
		if len(out) >= treeSessionsPerWorkspace {
			break
		}
		base := filepath.Base(si.Path)
		if store.IsSubagentTranscriptName(base) {
			continue
		}
		runtimeID := open[sessionstore.CanonicalSessionPath(si.Path)]
		if runtimeID == "" && hiddenRecoveryCopy(si, "") {
			continue
		}
		name := strings.TrimSuffix(base, ".jsonl")
		// A pane driving a copy keeps its own row: folding it away would hide
		// the conversation someone is looking at.
		folds := si.Recovered && runtimeID == ""
		if folds {
			if at, ok := lead[recoveryLineageRoot(si, byID)]; ok {
				out[at].Copies = append(out[at].Copies, treeSession{Path: si.Path, Name: name, Turns: si.Turns})
				continue
			}
		}
		title := titles.display(name+".jsonl", titleSource(si.Preview), sessionstore.SessionContentModTime(si.Path).UnixNano(), si.CustomTitle)
		if title == "" {
			title = previewTitle(si.Preview)
		}
		if folds {
			lead[recoveryLineageRoot(si, byID)] = len(out)
		}
		out = append(out, treeSession{
			Path: si.Path, Name: name, Title: title, Turns: si.Turns, RuntimeID: runtimeID, Archived: si.Archived, Unread: si.Unread,
		})
	}
	attachVersions(dir, out, h.openSessionsIn(root))
	return append(h.unlistedOpenSessions(root, out), out...)
}

// attachVersions hangs each conversation's earlier versions under its row. It
// runs after the rows exist because a version can be newer than its parent. A
// version a pane has open is left out: it is listed as that pane's own row.
func attachVersions(dir string, rows []treeSession, open map[string]string) {
	byParent, err := sessionstore.ListSessionVersions(dir)
	if err != nil || len(byParent) == 0 {
		return
	}
	for i := range rows {
		for _, v := range byParent[sessionstore.BranchID(rows[i].Path)] {
			if open[sessionstore.CanonicalSessionPath(v.Path)] != "" {
				continue
			}
			rows[i].Versions = append(rows[i].Versions, treeSession{
				Path: v.Path, Name: strings.TrimSuffix(filepath.Base(v.Path), ".jsonl"), Title: previewTitle(v.Preview), Turns: v.Turns,
			})
		}
	}
}

// unlistedOpenSessions are the conversations a pane in root holds that the
// listing does not show: a path is minted at the first submit, and the
// transcript is written after it, so a first turn in flight has no file yet.
// Without a row it can be neither found nor returned to while it runs.
func (h *Hub) unlistedOpenSessions(root string, listed []treeSession) []treeSession {
	seen := make(map[string]bool, len(listed))
	for _, row := range listed {
		seen[sessionstore.CanonicalSessionPath(row.Path)] = true
		for _, cp := range row.Copies {
			seen[sessionstore.CanonicalSessionPath(cp.Path)] = true
		}
	}
	var out []treeSession
	for _, rt := range h.localRuntimes() {
		ctrl := rt.Server.Controller()
		path := ctrl.SessionPath()
		canonical := sessionstore.CanonicalSessionPath(path)
		if canonical == "" || seen[canonical] || !drivesRoot(rt, root) {
			continue
		}
		seen[canonical] = true
		out = append(out, treeSession{
			Path: path, Name: strings.TrimSuffix(filepath.Base(path), ".jsonl"), RuntimeID: rt.ID,
		})
	}
	return out
}

// archiveSession changes catalog visibility without moving or deleting data.
// An idle pane on the session is closed first; one that is running is refused.
// Recovery siblings with another live pane stay active while the rest of the
// lineage is archived.
func (h *Hub) archiveSession(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path     string `json:"path"`
		Archived bool   `json:"archived"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		badBody(w)
		return
	}
	path, err := filepath.Abs(strings.TrimSpace(body.Path))
	if err != nil || !store.IsSessionTranscriptName(filepath.Base(path)) {
		refuse(w, http.StatusBadRequest, codeSessionBadPath, "the session path could not be resolved", nil)
		return
	}
	if id := h.openSessions()[sessionstore.CanonicalSessionPath(path)]; id != "" &&
		!h.releaseOrRefuse(w, r, "session.running", "this conversation is running; stop it first",
			h.panesWhere(func(rt *Runtime) bool { return rt.ID == id })) {
		return
	}
	if !h.ownsSessionDir(filepath.Dir(path)) {
		refuse(w, http.StatusForbidden, "session.outside_workspace", "path outside a known workspace", nil)
		return
	}
	open := h.openSessions()
	excluded := make([]string, 0, len(open))
	for openPath := range open {
		excluded = append(excluded, openPath)
	}
	if err := sessionstore.SetSessionLineageArchived(path, body.Archived, excluded...); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// importLegacySessions is the user-facing recovery path for old installations.
// Unknown historical workspaces fall back to the workspace the user selected,
// so a successful import is never stranded outside the desktop tree.
func (h *Hub) importLegacySessions(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path      string `json:"path"`
		Workspace string `json:"workspace"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		badBody(w)
		return
	}
	workspace, err := resolveWorkspaceDir(strings.TrimSpace(body.Workspace))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	known := ""
	for _, ref := range h.roots() {
		if ref.dir == workspace {
			known = ref.dir
			break
		}
	}
	if known == "" {
		refuse(w, http.StatusForbidden, "workspace.unknown", "select a known workspace for recovered sessions", nil)
		return
	}
	result := migration.RunLegacySessionImportInto(strings.TrimSpace(body.Path), SessionDirFor(known), legacyImportLog)
	count := 0
	for _, imported := range result.SessionImports {
		count += imported.Count
	}
	skipped := make([]legacySkipView, 0, len(result.SessionSkips))
	for _, skip := range result.SessionSkips {
		skipped = append(skipped, legacySkipView{Source: skip.Source, Name: skip.Name, Path: skip.Path, Reason: string(skip.Reason)})
	}
	writeJSON(w, legacyImportView{
		Summary: result.Summary(), Imported: count, Warnings: len(result.SessionErrs) + len(skipped),
		Recognised: !result.Unrecognised, Skipped: skipped,
	})
}

// legacySkipView is one 1.x session the import left in place. Reason is a
// sessionstore.SkipReason code; Path is the untouched entry in the source.
type legacySkipView struct {
	Source string `json:"source"`
	Name   string `json:"name"`
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type legacyImportView struct {
	Summary    string           `json:"summary"`
	Imported   int              `json:"imported"`
	Warnings   int              `json:"warnings"`
	Recognised bool             `json:"recognised"`
	Skipped    []legacySkipView `json:"skipped"`
}

var legacyImportLog = event.FuncSink(func(e event.Event) {
	if e.Level == event.LevelWarn {
		slog.Warn("serve: legacy import", "detail", e.Text)
	}
})

// recoveryLineageRoot names the conversation a copy belongs to. The stamped
// root is authoritative: walking parents instead splits one chain into a row
// per reclaimed middle link, which is precisely what GC leaves behind.
func recoveryLineageRoot(si sessionstore.SessionInfo, byID map[string]sessionstore.SessionInfo) string {
	if root := strings.TrimSpace(si.RecoveryRootID); root != "" {
		return root
	}
	id := sessionstore.BranchID(si.Path)
	for range maxRecoveryLineageWalk {
		info, ok := byID[id]
		if !ok || !info.Recovered {
			return id
		}
		parent := strings.TrimSpace(info.ParentID)
		if parent == "" || parent == id {
			return id
		}
		id = parent
	}
	return id
}

// addWorkspace remembers a folder without opening it. Adding and opening are
// separate acts: the sidebar lists what you work on, panes are what you drive.
func (h *Hub) addWorkspace(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		badBody(w)
		return
	}
	dir, err := resolveWorkspaceDir(strings.TrimSpace(body.Path))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := addRememberedWorkspace(r.Context(), dir); err != nil {
		if errors.Is(err, errWorkspaceListFull) {
			refuse(w, http.StatusConflict, "workspace.limit_reached", err.Error(), nil)
		} else {
			writeErr(w, http.StatusInternalServerError, err)
		}
		return
	}
	writeJSON(w, treeWorkspace{Root: dir, Name: fileutil.RootName(dir), Sessions: []treeSession{}})
}

// removeWorkspace drops a folder from the sidebar. Nothing on disk is touched —
// the sessions stay where they are and the folder can be added back.
func (h *Hub) removeWorkspace(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		badBody(w)
		return
	}
	dir := strings.TrimSpace(body.Path)
	if dir == "" {
		missingField(w, "path")
		return
	}
	inFolder := func(rt *Runtime) bool { return drivesRoot(rt, dir) }
	if !h.releaseOrRefuse(w, r, "workspace.running", "a conversation in this folder is running; stop it first", h.panesWhere(inFolder)) {
		return
	}
	forgetWorkspace(dir)
	w.WriteHeader(http.StatusNoContent)
}

// removeSession deletes a conversation the sidebar lists. A pane showing it is
// closed first, which lets the pane tear down its own jobs; one mid-turn is
// refused rather than pulled out from under the work.
func (h *Hub) removeSession(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		badBody(w)
		return
	}
	path, err := filepath.Abs(strings.TrimSpace(body.Path))
	if err != nil || !store.IsSessionTranscriptName(filepath.Base(path)) {
		refuse(w, http.StatusBadRequest, codeSessionBadPath, "the session path could not be resolved", nil)
		return
	}
	if id := h.openSessions()[sessionstore.CanonicalSessionPath(path)]; id != "" &&
		!h.releaseOrRefuse(w, r, "session.running", "this conversation is running; stop it first",
			h.panesWhere(func(rt *Runtime) bool { return rt.ID == id })) {
		return
	}
	dir := filepath.Dir(path)
	if !h.ownsSessionDir(dir) {
		refuse(w, http.StatusForbidden, "session.outside_workspace", "path outside a known workspace", nil)
		return
	}
	// Removing only the lead lets a covered recovery copy take its place and the
	// delete reads as having done nothing. Copies holding content their parent
	// lacks, and any a pane has open, are left alone.
	open := h.openSessions()
	excluded := make([]string, 0, len(open))
	for openPath := range open {
		excluded = append(excluded, openPath)
	}
	paths := []string{path}
	sibs, err := sessionstore.RecoveryLineagePaths(path, excluded...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	for _, p := range sibs {
		if p != path && sessionstore.RecoveryBranchCoveredByParent(p, dir) {
			paths = append(paths, p)
		}
	}
	// Every guard is taken before anything is erased, so a held copy refuses the
	// request with the lead and the rest intact. The lead goes last.
	guards := make([]*sessionstore.SessionRemovalGuard, 0, len(paths))
	for _, p := range paths {
		guard, ok := acquireRemovalGuard(w, p)
		if !ok {
			for _, g := range guards {
				g.Release()
			}
			return
		}
		guards = append(guards, guard)
	}
	for i := range slices.Backward(paths) {
		if !eraseGuardedSession(w, dir, paths[i], guards[i]) {
			for _, g := range guards[:i] {
				g.Release()
			}
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// acquireRemovalGuard answers the request itself when the session is held,
// returning false.
func acquireRemovalGuard(w http.ResponseWriter, path string) (*sessionstore.SessionRemovalGuard, bool) {
	// A pane's current path is narrower than "anyone writing this file": a
	// recovery branch or a mid-rotation session is held without being one.
	// Taking the guard beats probing it, which leaves a window for a writer.
	guard, err := sessionstore.TryAcquireSessionRemovalGuard(path)
	if err != nil {
		var held *sessionstore.SessionLeaseError
		if errors.As(err, &held) {
			if who := sessionHolder(held); who != nil {
				busy(w, "session.in_use_by", "another process holds this conversation open", who)
				return nil, false
			}
			busy(w, "session.in_use", "this conversation is still being written to", nil)
			return nil, false
		}
		writeErr(w, http.StatusInternalServerError, err)
		return nil, false
	}
	return guard, true
}

// eraseGuardedSession erases one transcript under its guard and answers the
// request itself when it cannot, returning false with the guard released.
func eraseGuardedSession(w http.ResponseWriter, dir, path string, guard *sessionstore.SessionRemovalGuard) bool {
	if err := removeSessionFiles(dir, path); err != nil {
		guard.Release()
		writeErr(w, http.StatusInternalServerError, err)
		return false
	}
	if err := guard.RemoveSidecarsAndRelease(); err != nil {
		// The conversation is already gone; a surviving lock file is stale
		// bookkeeping, not a failed delete.
		slog.Warn("serve: session removed, lock files survived", "path", path, "err", err)
	}
	return true
}

// sessionHolder names the process holding a conversation, and only when it is
// not this one. A lease this process holds means a write is in flight here,
// which is a different cause and carries a different code.
func sessionHolder(held *sessionstore.SessionLeaseError) map[string]any {
	if held == nil || held.Info == nil || held.Info.PID == 0 || held.Info.PID == os.Getpid() {
		return nil
	}
	return map[string]any{"pid": held.Info.PID, "host": held.Info.Hostname}
}

// exportSession returns the transcript only after applying the same workspace
// boundary as rename and removal. The shell owns where the copy is saved; the
// kernel owns which path is safe to read.
func (h *Hub) exportSession(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		badBody(w)
		return
	}
	path, err := filepath.Abs(strings.TrimSpace(body.Path))
	if err != nil || !store.IsSessionTranscriptName(filepath.Base(path)) {
		refuse(w, http.StatusBadRequest, codeSessionBadPath, "the session path could not be resolved", nil)
		return
	}
	if !h.ownsSessionDir(filepath.Dir(path)) {
		refuse(w, http.StatusForbidden, "session.outside_workspace", "path outside a known workspace", nil)
		return
	}
	content, err := os.ReadFile(path)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}{Name: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), Content: string(content)})
}

// ownsSessionDir reports whether dir is the session directory of a workspace
// this hub lists, which is the boundary a delete may not reach past.
func (h *Hub) ownsSessionDir(dir string) bool {
	for _, ref := range h.roots() {
		if known, err := filepath.Abs(SessionDirFor(ref.dir)); err == nil && known == dir {
			return true
		}
	}
	return false
}

// rootRef is one sidebar folder and whether the user ever chose it.
type rootRef struct {
	dir        string
	remembered bool
}

// roots returns the sidebar's folders: everything remembered, plus whatever a
// pane is driving, so an open workspace can never be missing from the tree.
func (h *Hub) roots() []rootRef {
	paths := Workspaces()
	saved := map[string]bool{}
	for _, dir := range paths {
		if dir = strings.TrimSpace(dir); dir != "" {
			saved[dir] = true
		}
	}
	seen := map[string]bool{}
	var out []rootRef
	add := func(dir string) {
		dir = strings.TrimSpace(dir)
		if dir == "" || seen[dir] {
			return
		}
		seen[dir] = true
		out = append(out, rootRef{dir: dir, remembered: saved[dir]})
	}
	for _, dir := range paths {
		add(dir)
	}
	for _, rt := range h.localRuntimes() {
		add(rt.Server.Controller().WorkspaceRoot())
	}
	return out
}

// openSessions maps canonical session paths to the runtime driving them.
// drivesRoot reports whether rt is a local pane working in root.
func drivesRoot(rt *Runtime, root string) bool {
	return rt.Local() && rt.Server.Controller().WorkspaceRoot() == root
}

// openSessionsIn is openSessions for the panes drivesRoot accepts: the ones
// unlistedOpenSessions will give a row of their own under root.
func (h *Hub) openSessionsIn(root string) map[string]string {
	out := map[string]string{}
	for _, rt := range h.localRuntimes() {
		if !drivesRoot(rt, root) {
			continue
		}
		if path := sessionstore.CanonicalSessionPath(rt.Server.Controller().SessionPath()); path != "" {
			out[path] = rt.ID
		}
	}
	return out
}

func (h *Hub) openSessions() map[string]string {
	out := map[string]string{}
	for _, rt := range h.localRuntimes() {
		if path := sessionstore.CanonicalSessionPath(rt.Server.Controller().SessionPath()); path != "" {
			out[path] = rt.ID
		}
	}
	return out
}

func (h *Hub) rootPanes(root string) int {
	n := 0
	for _, rt := range h.localRuntimes() {
		if rt.Server.Controller().WorkspaceRoot() == root {
			n++
		}
	}
	return n
}

// titleCacheFor keeps one reader per project directory. Titles are file-name
// keyed and unique only within a project, so the caches must not be shared.
func (h *Hub) titleCacheFor(dir string) *titleCache {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.titles == nil {
		h.titles = map[string]*titleCache{}
	}
	if c := h.titles[dir]; c != nil {
		return c
	}
	c := newTitleCache(dir)
	h.titles[dir] = c
	return c
}

// workspaceRootForSession recovers which folder a transcript belongs to from
// its own sidecar, for an open request that names a session but no root.
func workspaceRootForSession(path string) string {
	meta, ok, err := sessionstore.LoadBranchMeta(strings.TrimSpace(path))
	if err != nil || !ok {
		return ""
	}
	return strings.TrimSpace(meta.WorkspaceRoot)
}
