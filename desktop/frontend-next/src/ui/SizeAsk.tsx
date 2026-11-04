import { useState, type ReactNode } from "react";
import { t } from "../i18n";
import { ToastRow } from "./ToastRow";
import { host } from "../port/host";
import type { Appearance } from "../port/port";

/** Set once the question has been answered, whichever way. */
export const ASKED = "rx-size-asked";

// A display this wide at the OS's 100% scaling draws the interface at the
// size meant for a smaller screen. The display only decides whether to ask:
// it never sets a value.
const WIDE_CSS_PX = 2400;

const read = () => {
  try {
    return localStorage.getItem(ASKED) === "1";
  } catch {
    return true;
  }
};
const remember = () => {
  try {
    localStorage.setItem(ASKED, "1");
  } catch {
    // Not asking again is then lost with the storage; asking twice is harmless.
  }
};

type Phase = { at: "ask" } | { at: "preview"; was: number } | { at: "done" };

/** The one-time "is the interface too small?" notice. A size the person chose
 *  is never in question, and nothing changes until they pick one; picking
 *  applies it at once so it can be judged, and it can be put back. */
export function useSizeAsk(look: Appearance, onLook: (next: Appearance) => void): { open: boolean; node: ReactNode } {
  const [eligible] = useState(() => host().inShell() && !read() && window.devicePixelRatio === 1 && window.screen.width >= WIDE_CSS_PX && (look.zoom || 1) === 1);
  const [phase, setPhase] = useState<Phase>({ at: "ask" });
  if (!eligible || phase.at === "done") return { open: false, node: null };
  const close = () => {
    remember();
    setPhase({ at: "done" });
  };
  const apply = (zoom: number) => {
    remember();
    onLook({ ...look, zoom });
    setPhase({ at: "preview", was: look.zoom || 1 });
  };
  if (phase.at === "preview") {
    const was = phase.was;
    const now = Math.round((look.zoom || 1) * 100);
    return {
      open: true,
      node: (
        <ToastRow kind="size" role="status" className="size-ask" actions={<>
          <button className="act ghost" data-action="size.restore" onClick={() => { onLook({ ...look, zoom: was }); close(); }}>{t("safety.restore_n", { n: Math.round(was * 100) })}</button>
          <button className="act" data-action="size.keep" onClick={close}>{t("safety.ok")}</button>
        </>}>{t("safety.the_interface_is_now_n", { n: now })}</ToastRow>
      ),
    };
  }
  return {
    open: true,
    node: (
      <ToastRow kind="size" role="status" className="size-ask" stacked actions={<>
        <button className="act" data-action="size.set" data-value="1.25" onClick={() => apply(1.25)}>125%</button>
        <button className="act" data-action="size.set" data-value="1.5" onClick={() => apply(1.5)}>150%</button>
        <button className="act ghost" data-action="size.keep" onClick={close}>{t("common.no_thanks")}</button>
      </>}>{t("safety.is_the_interface_too_small")}</ToastRow>
    ),
  };
}

export function SizeAsk({ look, onLook }: { look: Appearance; onLook: (next: Appearance) => void }) {
  return <>{useSizeAsk(look, onLook).node}</>;
}
