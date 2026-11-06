import { useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, SessionStatus } from "../port/port";
import { StudioIcon } from "./StudioIcon";

export type PostureNoteKind = "trust" | "unconfined";

const UNCONFINED_SEEN = "rx-posture-unconfined-seen";

// Which note a session on its default posture owes. The trust question is the
// terminal's: asked only where the sandbox would confine what trusting allows.
// Without a sandbox trusting changes nothing, so what is said is why it asks.
export function postureNote(status: SessionStatus | null): PostureNoteKind | null {
  const d = status?.approvalDefault;
  if (!d?.defaulted || status?.toolApprovalMode !== "ask") return null;
  if (!d.writesConfined) return "unconfined";
  return d.trust === "" && d.trustable ? "trust" : null;
}

function seenUnconfined(): boolean {
  try {
    return localStorage.getItem(UNCONFINED_SEEN) === "1";
  } catch {
    return false;
  }
}

function markUnconfinedSeen() {
  try {
    localStorage.setItem(UNCONFINED_SEEN, "1");
  } catch {
    // A viewer convenience: without storage the note returns next session.
  }
}

export function PostureNote({ port, status, onChanged }: {
  port: AgentPort;
  status: SessionStatus | null;
  onChanged: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [refused, setRefused] = useState("");
  const [hidden, setHidden] = useState(seenUnconfined);
  const kind = postureNote(status);
  if (!kind || (kind === "unconfined" && hidden)) return null;

  const act = (run: () => Promise<void>) => {
    if (busy) return;
    setBusy(true);
    setRefused("");
    run().then(onChanged).catch((e: unknown) => setRefused(reason(e))).finally(() => setBusy(false));
  };

  if (kind === "trust") {
    return (
      <div className="rtbar posturenote" data-kind="trust" role="group" aria-label={t("composer.trust_this_folder")}>
        <StudioIcon name="shield" />
        <span className="t" title={refused || t("composer.once_trusted_edits_and_commands")}>{refused || t("composer.trust_this_folder_2")}</span>
        <button data-action="workspace.trust" data-value="trusted" disabled={busy} onClick={() => act(() => port.decideWorkspaceTrust("trusted"))}>{t("common.trust_this_folder")}</button>
        <button data-action="workspace.trust" data-value="declined" disabled={busy} onClick={() => act(() => port.decideWorkspaceTrust("declined"))}>{t("composer.not_now")}</button>
      </div>
    );
  }
  return (
    <div className="rtbar posturenote" data-kind="unconfined" data-lvl="warn" role="status">
      <StudioIcon name="info" />
      <span className="t" title={refused || t("composer.no_os_sandbox_is_in")}>{refused || t("composer.no_sandbox_ask_first")}</span>
      <button data-action="tool-approval.mode" data-value="auto" disabled={busy} onClick={() => act(() => port.setApprovalMode("auto"))}>{t("composer.auto_approve")}</button>
      <button data-action="posture-note.dismiss" onClick={() => { markUnconfinedSeen(); setHidden(true); }}>{t("common.got_it")}</button>
    </div>
  );
}
