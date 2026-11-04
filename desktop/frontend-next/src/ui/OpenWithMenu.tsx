import { useEffect, useState } from "react";
import { blockedInProject, blockedNote, openWithName } from "./openwith_label";
import type { HubPort, OpenWithTarget } from "../port/hub";
import { host } from "../port/host";
import { t } from "../i18n";
import { StudioIcon } from "./StudioIcon";

// The programs this machine can open the project in. Which exist is the kernel's
// table, asked when the menu opens: what is installed can change while the window
// stays open. Offered only where the kernel's own machine can start programs.
export function OpenWithMenu({ hub, root, onError }: {
  hub: Pick<HubPort, "openWith" | "openWithTargets">;
  root: string;
  onError: (e: unknown) => void;
}) {
  const shell = host().revealsFiles();
  const [open, setOpen] = useState(false);
  const [targets, setTargets] = useState<OpenWithTarget[]>([]);
  useEffect(() => {
    if (!open) return;
    let alive = true;
    void hub.openWithTargets().then((list) => alive && setTargets(list.filter((x) => x.available || blockedInProject(x)))).catch(() => {});
    return () => { alive = false; };
  }, [hub, open]);
  if (!shell || !root) return null;
  return (
    <div
      className="openwith"
      data-action-keydown="layer.dismiss"
      onKeyDown={(e) => { if (e.key === "Escape" && open) { e.stopPropagation(); setOpen(false); } }}
      onBlur={(e) => { if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setOpen(false); }}
    >
      <button
        className="openwith-btn"
        data-action="workbench.open-with-menu"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={t("workbench.open_the_project_in_another")}
        title={t("workbench.open_the_project_in_another")}
        onClick={() => setOpen((v) => !v)}
      >
        <StudioIcon name="external" /><StudioIcon name="down" />
      </button>
      {open && (
        <div className="openwith-menu" role="menu" aria-label={t("workbench.open_the_project_in_another")}>
          {targets.length === 0 && <small>{t("workbench.no_supported_program_found")}</small>}
          {targets.map((target) => (
            <button
              key={target.id}
              role="menuitem"
              data-action="workspace.open-with"
              data-target={root}
              data-value={target.id}
              disabled={!target.available}
              title={target.available ? undefined : blockedNote(target)}
              onClick={() => {
                setOpen(false);
                hub.openWith(root, target.id).catch(onError);
              }}
            >
              <span>{t("common.open_with_name", { name: openWithName(target) })}</span>
              {!target.available && <small>{t("common.inside_a_project_folder")}</small>}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
