import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { AgentPort, WorkspaceFile } from "../port/port";
import type { GitRepoRow } from "../port/workspace";
import { locate } from "../state/gittree";
import { WORKSPACE_FILE_MISSING, reason } from "../i18n/kernel";
import { HttpError } from "../port/port";
import { parseGrantedFile } from "./grantedfiles";
import { t } from "../i18n";

type Entry = {
  file: WorkspaceFile | null; draft: string; diff: string; error: string; missing: boolean;
  errorCode: string;
  pending: "" | "read" | "save"; issued: number;
};
const blank = (): Entry => ({ file: null, draft: "", diff: "", error: "", errorCode: "", missing: false, pending: "", issued: 0 });
const dirty = (entry: Entry) => !!entry.file && entry.draft !== entry.file.content;

export function useWorkbenchFiles({ port, workspaceRoot, repos = [], path, openPaths, shown, refresh, poll }: {
  port: AgentPort; workspaceRoot: string; repos?: GitRepoRow[]; path: string; openPaths: string[]; shown: boolean; refresh: string; poll: boolean;
}) {
  const entries = useMemo(() => new Map<string, Entry>(), [port, workspaceRoot]);
  const current = useRef<Map<string, Entry> | null>(entries);
  current.current = entries;
  const [version, render] = useState(0);
  const notify = useCallback(() => { if (current.current === entries) render((n) => n + 1); }, [entries]);
  const repoList = useRef(repos);
  repoList.current = repos;
  const entry = path ? entries.get(path) ?? blank() : blank();
  if (path && !entries.has(path)) entries.set(path, entry);

  useEffect(() => {
    current.current = entries;
    return () => { current.current = null; };
  }, [entries]);
  useEffect(() => {
    for (const [path, entry] of entries) {
      if (!openPaths.includes(path) && !dirty(entry) && entry.pending !== "save") entries.delete(path);
    }
  }, [entries, openPaths, version]);

  const read = useCallback((quiet = false) => {
    if (!path) return;
    const entry = entries.get(path)!;
    let live = true;
    if (entry.pending === "save" || dirty(entry) || (entry.file && !shown) || (quiet && entry.pending === "read")) return;
    const ticket = ++entry.issued;
    let changed = !quiet || !entry.file;
    void (async () => {
      const valid = () => live && current.current === entries && ticket === entry.issued;
      entry.pending = "read";
      if (!entry.file) { entry.error = ""; entry.missing = false; }
      if (changed) notify();
      try {
        const granted = parseGrantedFile(path);
        const [file, patch] = await Promise.all([
          granted ? port.workspaceFile(granted.rel, granted.root) : port.workspaceFile(path),
          quiet || granted ? Promise.resolve(null) : diffOf(port, repoList.current, path),
        ]);
        if (!valid() || dirty(entry)) return;
        changed ||= entry.file?.content !== file.content || entry.file?.revision !== file.revision || !!entry.error;
        entry.file = file;
        entry.draft = file.content;
        entry.error = "";
        entry.errorCode = "";
        entry.missing = false;
        if (!quiet) entry.diff = patch?.diff ?? "";
      } catch (e) {
        const code = e instanceof HttpError ? e.reason?.code ?? "" : "";
        const unavailable = [WORKSPACE_FILE_MISSING, "workspace.root_not_granted", "workspace.preview_denied", "workspace.file_unreadable"].includes(code);
        if (valid() && !dirty(entry) && (!quiet || unavailable)) {
          changed = true;
          entry.error = reason(e);
          entry.errorCode = code;
          entry.missing = e instanceof HttpError && e.reason?.code === WORKSPACE_FILE_MISSING;
          entry.file = null;
          entry.draft = "";
        }
      } finally {
        if (valid()) { entry.pending = ""; if (changed) notify(); }
      }
    })();
    return () => {
      live = false;
      if (ticket === entry.issued) entry.pending = "";
    };
  }, [port, entries, path, shown, notify]);
  useEffect(() => read(), [read, refresh]);
  useEffect(() => {
    if (!poll || !shown) return;
    let cancel: (() => void) | undefined;
    const timer = window.setInterval(() => { cancel = read(true) ?? cancel; }, 3000);
    return () => { cancel?.(); window.clearInterval(timer); };
  }, [read, poll, shown]);
  const isDirty = dirty(entry), observedVersion = entry.file?.observedVersion, saving = entry.pending === "save";
  useEffect(() => {
    if (!shown || !isDirty || !observedVersion || saving) return;
    const controller = new AbortController();
    let busy = false;
    const inspect = async () => {
      if (busy) return;
      busy = true;
      try {
        const held = parseGrantedFile(path);
        const info = await port.workspacePreviewInfo({ path: held?.rel ?? path, root: held?.root }, controller.signal);
        if (!controller.signal.aborted && info.version !== entry.file?.observedVersion) {
          const error = t("磁盘内容已变化，未保存的编辑已保留");
          if (entry.error !== error) { entry.error = error; notify(); }
        }
      } catch (e) {
        if (!controller.signal.aborted) {
          const error = reason(e);
          if (entry.error !== error) { entry.error = error; notify(); }
        }
      } finally { busy = false; }
    };
    void inspect();
    const timer = window.setInterval(() => void inspect(), 3000);
    return () => { controller.abort(); window.clearInterval(timer); };
  }, [port, entry, path, shown, refresh, isDirty, observedVersion, saving, notify]);

  const setDraft = (draft: string) => {
    if (current.current !== entries || !entry.file || entry.file.readOnly) return;
    entry.draft = draft;
    notify();
  };
  // Saving by path, because a tab can be closed while another file is the one
  // on screen: the confirmation that closes it must write that file's draft,
  // not whatever the panel happens to be showing.
  const saveEntry = async (target: string): Promise<boolean> => {
    const held = entries.get(target);
    if (current.current !== entries || !held || !held.file || held.file.readOnly || !dirty(held) || held.pending === "save") return false;
    const sent = { ...held.file, content: held.draft };
    const ticket = ++held.issued;
    held.pending = "save";
    held.error = "";
    notify();
    try {
      const file = await port.saveWorkspaceFile(sent);
      if (current.current !== entries || ticket !== held.issued) return false;
      held.file = file;
      if (held.draft === sent.content) held.draft = file.content;
      return true;
    } catch (e) {
      if (current.current === entries && ticket === held.issued) held.error = reason(e);
      return false;
    } finally {
      if (current.current === entries && ticket === held.issued) { held.pending = ""; notify(); }
    }
  };
  const save = () => saveEntry(path);
  // Closing without saving drops the draft, so reopening the file reads the
  // disk again instead of resurrecting edits the person just refused.
  const discard = (target: string) => {
    const held = entries.get(target);
    if (current.current !== entries || !held || !held.file) return;
    held.draft = held.file.content;
    held.error = "";
    notify();
  };
  // Every open file with edits, so a tab can show it without being the active one.
  const dirtyPaths = [...entries].filter(([, held]) => dirty(held)).map(([path]) => path);
  return { ...entry, dirtyPaths, busy: entry.pending === "save" || (!entry.file && entry.pending === "read"), setDraft, save, savePath: saveEntry, discard };
}

// The diff of a workspace-relative path, read from the repository that holds it.
function diffOf(port: AgentPort, repos: GitRepoRow[], path: string) {
  const at = locate(repos, path);
  return (at.repo ? port.changeDiff(at.rel, at.repo) : port.changeDiff(path)).catch(() => null);
}
