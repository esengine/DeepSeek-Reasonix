import { useState, type ReactNode } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { HubPort, TreeSession, TreeWorkspace } from "../port/hub";
import { MoveSessionDialog } from "./MoveSessionDialog";

/** Moving a conversation into another project: the request, the dialog that asks
 *  where, and the refusal it shows in the kernel's own words. After a move the
 *  tree is read again rather than patched. */
export function useSessionMoveTo(hub: HubPort, tree: TreeWorkspace[], reload: () => Promise<void>, label: (s: TreeSession) => string, addProject?: () => void) {
  const [moving, setMoving] = useState<{ session: TreeSession; from: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [note, setNote] = useState("");
  const close = () => { setMoving(null); setError(""); setNote(""); };
  const go = async (target: string) => {
    if (!moving) return;
    setBusy(true);
    setError("");
    try {
      const moved = await hub.moveSessionTo(moving.session.path, target);
      await reload();
      if (moved.originalRemoved) close();
      else setNote(t("session.the_conversation_was_moved_but"));
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };
  const dialog: ReactNode = moving && (
    <MoveSessionDialog session={moving.session} label={label(moving.session)} busy={busy} error={error} note={note}
      projects={tree.filter((w) => w.remembered && w.root !== moving.from)} onMove={(target) => void go(target)} onClose={close} onAddProject={addProject} />
  );
  return { request: (session: TreeSession, from: string) => { setError(""); setNote(""); setMoving({ session, from }); }, dialog };
}
