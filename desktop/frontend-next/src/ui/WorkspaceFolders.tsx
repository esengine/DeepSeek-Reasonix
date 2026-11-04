import { useEffect, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, WorkspaceRoots } from "../port/port";

// Folders this project may write to besides its own, kept in the user's home
// and never in the checkout. Approval mode is a different question: it decides
// whether to ask, not where a write can land, so the two are said apart here.
export function WorkspaceFolders({ port, onChanged }: { port: AgentPort; onChanged: () => void }) {
  const [roots, setRoots] = useState<WorkspaceRoots | null>(null);
  const [draft, setDraft] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [loadFailed, setLoadFailed] = useState(false);
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let alive = true;
    setLoadFailed(false);
    port.workspaceRoots().then((r) => alive && setRoots(r)).catch(() => alive && setLoadFailed(true));
    return () => { alive = false; };
  }, [port, attempt]);

  if (loadFailed) {
    return (
      <div className="sec" id="set-workspace-folders">
        <h3>{t("session.extra_folders_for_this_project")}</h3>
        <div className="find" data-lvl="warn" role="status">
          <span className="t">{t("session.could_not_read_this_projects")}</span>
          <button className="act" data-action="folders.retry" onClick={() => setAttempt((n) => n + 1)}>{t("common.retry")}</button>
        </div>
      </div>
    );
  }
  if (!roots) return null;

  const change = async (apply: () => Promise<WorkspaceRoots>) => {
    setBusy(true);
    setError("");
    try {
      setRoots(await apply());
      onChanged();
      return true;
    } catch (e) {
      setError(reason(e));
      return false;
    } finally {
      setBusy(false);
    }
  };
  const add = async (path: string) => {
    const dir = path.trim();
    if (dir && (await change(() => port.addWorkspaceRoot(dir)))) setDraft("");
  };
  // Choosing only fills the field: granting write access is the Add button's.
  const pick = async () => {
    const dir = await port.pickFolder().catch(() => null);
    if (dir) setDraft(dir);
  };

  return (
    <div className="sec" id="set-workspace-folders">
      <h3>{t("session.extra_folders_for_this_project")}</h3>
      <p className="note">
        {t("session.the_agent_can_only_modify")}
      </p>
      <p className="note">
        {t("session.once_added_the_agent_can")}
      </p>
      <p className="note">
        {t("session.allow_everything_only_decides_whether")}
      </p>
      <div className="extra">
        <div className="sublb">{t("session.project_folders")}</div>
        <div className="kv">
          <span className="k">{t("session.this_project")}</span>
          <span className="v"><code>{roots.primary || "—"}</code></span>
        </div>
        {roots.extra.map((p) => (
          <div className="prule" key={p}>
            <code>{p}</code>
            <button
              className="act ghost"
              data-action="folders.remove"
              data-target={p}
              disabled={busy}
              aria-label={t("session.remove_folder_path", { path: p })}
              onClick={() => void change(() => port.removeWorkspaceRoot(p))}
            >
              {t("common.remove")}
            </button>
          </div>
        ))}
        <div className="prule" data-add="">
          <input
            value={draft}
            data-action-keydown="folders.add"
            aria-label={t("session.folder_path_to_add")}
            placeholder={t("session.absolute_path_for_example_users")}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && void add(draft)}
          />
          <button className="act" data-action="folders.pick" disabled={busy} onClick={() => void pick()}>
            {t("common.choose_a_folder")}
          </button>
          <button className="act" data-action="folders.add" disabled={busy || !draft.trim()} onClick={() => void add(draft)}>
            {t("common.add")}
          </button>
        </div>
      </div>
      {roots.extra.length > 0 && (
        <p className="note">{t("session.after_a_removal_panes_that")}</p>
      )}
      {(roots.pending ?? 0) > 0 && (
        <p className="note" role="status">
          {t("session.n_panes_are_still_running", { n: roots.pending ?? 0 })}
        </p>
      )}
      {error && (
        <div className="find" data-lvl="err" role="alert">
          <span className="why">{error}</span>
        </div>
      )}
    </div>
  );
}
