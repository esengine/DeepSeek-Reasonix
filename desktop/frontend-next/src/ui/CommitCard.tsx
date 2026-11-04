import { useEffect, useRef, useState } from "react";
import type { AgentPort, CommitProposal, CommitResult } from "../port/port";
import type { CommitDraftFailure } from "../port/workspace";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";

type Phase =
  | { phase: "idle" }
  | { phase: "drafting" }
  | { phase: "ready"; proposal: CommitProposal; message: string; ack: boolean; committing: boolean; error?: string }
  | { phase: "done"; result: CommitResult }
  | { phase: "failed"; error: string };

// Why there is no drafted message. The staged set is still shown, so the person
// writes it themselves; each reason says what to expect from drafting again.
const DRAFT_NOTE: Record<CommitDraftFailure, string> = {
  no_model: "workbench.this_session_has_no_model",
  timeout: "workbench.drafting_timed_out_write_the",
  no_answer: "workbench.the_model_gave_no_commit",
  model_failed: "workbench.the_model_failed_to_draft",
};

// A proposal, not an action: the model drafts for what is staged, the person
// edits, and only the confirm button records a local commit. Nothing here stages
// a file or pushes.
export function useCommitCard(port: AgentPort, onCommitted: () => void, repo?: string) {
  const [state, setState] = useState<Phase>({ phase: "idle" });
  const pending = useRef<AbortController | null>(null);
  useEffect(() => () => pending.current?.abort(), []);

  const draft = () => {
    pending.current?.abort();
    const ac = new AbortController();
    pending.current = ac;
    setState({ phase: "drafting" });
    (repo ? port.proposeCommit(ac.signal, repo) : port.proposeCommit(ac.signal)).then(
      (proposal) =>
        !ac.signal.aborted && setState({ phase: "ready", proposal, message: proposal.message, ack: false, committing: false }),
      (e) => !ac.signal.aborted && setState({ phase: "failed", error: reason(e) }),
    );
  };
  const close = () => {
    pending.current?.abort();
    pending.current = null;
    setState({ phase: "idle" });
  };

  const button = (
    <button type="button" className="btn sm commit-open" data-action="commit.draft" disabled={state.phase !== "idle"} onClick={draft}>
      {t("workbench.commit")}
    </button>
  );
  if (state.phase === "idle") return { button, card: null };

  if (state.phase === "drafting") {
    return {
      button,
      card: (
      <section className="commit-card" data-phase="drafting" aria-label={t("workbench.commit_message")}>
        <p className="refine-wait" role="status">{t("workbench.reading_the_staged_changes_and")}</p>
        <footer>
          <button type="button" className="btn sm" data-weak="" data-action="commit.close" onClick={close}>{t("common.cancel")}</button>
        </footer>
      </section>
      ),
    };
  }

  if (state.phase === "failed") {
    return {
      button,
      card: (
      <section className="commit-card" data-phase="failed" aria-label={t("workbench.commit_message")}>
        <p className="refine-error" role="alert">{state.error}</p>
        <footer>
          <button type="button" className="btn sm" data-action="commit.draft" onClick={draft}>{t("workbench.draft_commit_message")}</button>
          <button type="button" className="btn sm" data-weak="" data-action="commit.close" onClick={close}>{t("common.cancel")}</button>
        </footer>
      </section>
      ),
    };
  }

  if (state.phase === "done") {
    return {
      button,
      card: (
      <section className="commit-card" data-phase="done" aria-label={t("workbench.commit_message")}>
        <p className="commit-done" role="status">
          {t("workbench.committed_hash_subject", { hash: state.result.hash.slice(0, 7), subject: state.result.subject })}
        </p>
        <small>{t("workbench.recorded_locally_only_nothing_is")}</small>
        <footer>
          <button type="button" className="btn sm" data-weak="" data-action="commit.close" onClick={close}>{t("common.collapse")}</button>
        </footer>
      </section>
      ),
    };
  }

  const { proposal, message, ack, committing, error } = state;
  const sensitive = proposal.files.filter((f) => f.sensitive).map((f) => f.path);
  const warned = sensitive.length > 0 || proposal.contentSecrets;
  const set = (patch: Partial<typeof state>) => setState({ ...state, ...patch });
  const confirm = () => {
    set({ committing: true, error: undefined });
    port
      .commitStaged({ ...(repo ? { repo } : {}), message, fingerprint: proposal.fingerprint, acknowledgeSecrets: ack })
      .then(
        (result) => {
          setState({ phase: "done", result });
          onCommitted();
        },
        (e) => setState({ ...state, committing: false, error: reason(e) }),
      );
  };

  return {
    button,
    card: (
    <section className="commit-card" data-phase="ready" aria-label={t("workbench.commit_message")}>
      <header>
        <b>{t("workbench.staged_files")}</b>
        <small>{t("workbench.n_files_staged", { n: proposal.files.length })}</small>
      </header>
      <ul className="commit-files">
        {proposal.files.map((f) => (
          <li key={f.path} data-sensitive={f.sensitive ? "" : undefined} title={f.path}>
            <em className="st" data-s={f.status}>{f.status}</em>
            <CommitPath path={f.path} />
            {f.insertions !== undefined && <i data-io="up">+{f.insertions}</i>}
            {f.deletions !== undefined && <i data-io="down">-{f.deletions}</i>}
          </li>
        ))}
      </ul>
      {proposal.truncated && <small>{t("workbench.the_staged_diff_is_long")}</small>}
      {warned && (
        <div className="commit-warn" role="alert">
          {sensitive.length > 0 && <p>{t("workbench.may_contain_secrets_files", { files: sensitive.join(", ") })}</p>}
          {proposal.contentSecrets && <p>{t("workbench.added_lines_contain_values_shaped")}</p>}
          <label>
            <input type="checkbox" data-action="commit.acknowledge" checked={ack} disabled={committing} onChange={(e) => set({ ack: e.target.checked })} />
            {t("workbench.i_confirm_these_can_be")}
          </label>
        </div>
      )}
      {proposal.draftFailure && <p className="refine-error" role="status" data-draft={proposal.draftFailure}>{t(DRAFT_NOTE[proposal.draftFailure])}</p>}
      <textarea
        className="commit-message"
        autoFocus={!!proposal.draftFailure}
        data-action="commit.edit"
        aria-label={t("workbench.commit_message")}
        value={message}
        rows={6}
        spellCheck={false}
        disabled={committing}
        onChange={(e) => set({ message: e.target.value })}
      />
      {error && <p className="refine-error" role="alert">{error}</p>}
      <small>{t("workbench.recorded_locally_only_nothing_is")}</small>
      <footer>
        <button
          type="button"
          className="btn sm"
          data-primary
          data-action="commit.confirm"
          disabled={committing || !message.trim() || (warned && !ack)}
          onClick={confirm}
        >
          {committing ? t("common.submitting") : t("workbench.commit_locally")}
        </button>
        <button type="button" className="btn sm" data-action="commit.draft" disabled={committing} onClick={draft}>
          {t("workbench.redraft")}
        </button>
        <button type="button" className="btn sm" data-weak="" data-action="commit.close" disabled={committing} onClick={close}>
          {t("common.cancel")}
        </button>
      </footer>
    </section>
  ),
  };
}

// Only the directory gives way when the pane is narrow, from its left, so its tail
// and the whole file name stay visible. The directory is rtl for that ellipsis side.
function CommitPath({ path }: { path: string }) {
  const cut = path.lastIndexOf("/") + 1;
  return (
    <span className="commit-path">
      {cut > 0 && <span className="commit-dir">{path.slice(0, cut)}</span>}
      <span className="commit-base">{path.slice(cut)}</span>
    </span>
  );
}
