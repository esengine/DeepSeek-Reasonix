import { useEffect, useState } from "react";
import { blockedInProject, blockedNote, openWithName } from "./openwith_label";
import type { HubPort, OpenWithTarget } from "../port/hub";
import { host } from "../port/host";
import { t } from "../i18n";
import { StudioIcon } from "./StudioIcon";

const LAST = "rx-open-with-last";

function readLast(): { id: string; name: string } | null {
  try {
    const got = JSON.parse(localStorage.getItem(LAST) ?? "null") as unknown;
    return got && typeof got === "object" && typeof (got as { id?: unknown }).id === "string" ? (got as { id: string; name: string }) : null;
  } catch {
    return null;
  }
}

function keepLast(target: OpenWithTarget) {
  try {
    localStorage.setItem(LAST, JSON.stringify({ id: target.id, name: openWithName(target) }));
  } catch {
    // A blocked store forgets the choice; the arrow still lists every program.
  }
}

function Tile({ id }: { id: string }) {
  const icon = id === "terminal" ? "terminal" : ["vscode", "cursor", "idea"].includes(id) ? "code" : "external";
  return <span className="openwith-tile" aria-hidden="true" data-program={id}><StudioIcon name={icon} /></span>;
}

const revealWord = (platform: string) =>
  t(platform === "darwin" ? "workbench.reveal_in_finder" : platform === "windows" ? "workbench.reveal_in_file_explorer" : "workbench.show_in_file_manager");

// The programs this machine can open the project in. Which exist is the kernel's
// table, asked when the menu opens: what is installed can change while the window
// stays open. Offered only where the kernel's own machine can start programs.
export function OpenWithMenu({ hub, root, platform = "", onError }: {
  hub: Pick<HubPort, "openWith" | "openWithTargets">;
  root: string;
  platform?: string;
  onError: (e: unknown) => void;
}) {
  const shell = host().revealsFiles();
  const [open, setOpen] = useState(false);
  const [last, setLast] = useState(readLast);
  const [targets, setTargets] = useState<OpenWithTarget[] | null>(null);
  useEffect(() => {
    if (!open) return;
    let alive = true;
    void hub.openWithTargets().then((list) => alive && setTargets(list.filter((x) => x.available || blockedInProject(x)))).catch(() => alive && setTargets([]));
    return () => { alive = false; };
  }, [hub, open]);
  if (!shell || !root) return null;
  const run = (target: OpenWithTarget) => {
    setOpen(false);
    if (target.id !== "file-manager") {
      keepLast(target);
      setLast(readLast());
    }
    hub.openWith(root, target.id).catch(onError);
  };
  const programs = (targets ?? []).filter((x) => x.id !== "file-manager");
  const files = targets?.find((x) => x.id === "file-manager" && x.available);
  return (
    <div
      className="openwith"
      data-action-keydown="layer.dismiss"
      onKeyDown={(e) => { if (e.key === "Escape" && open) { e.stopPropagation(); setOpen(false); } }}
      onBlur={(e) => { if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setOpen(false); }}
    >
      {last && (
        <button className="openwith-btn openwith-main" data-action="workspace.open-with" data-target={root} data-value={last.id}
          aria-label={t("common.open_with_name", { name: last.name })} title={t("common.open_with_name", { name: last.name })}
          onClick={() => hub.openWith(root, last.id).catch(onError)}>
          <Tile id={last.id} />
        </button>
      )}
      <button
        className="openwith-btn"
        data-action="workbench.open-with-menu"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={t("workbench.open_the_project_in_another")}
        title={t("workbench.open_the_project_in_another")}
        onClick={() => setOpen((v) => !v)}
      >
        {!last && <StudioIcon name="external" />}<StudioIcon name="down" />
      </button>
      {open && (
        <div className="openwith-menu" role="menu" aria-label={t("workbench.open_the_project_in_another")}>
          <div className="openwith-head">{t("workbench.open_the_project_in_another")}</div>
          {targets && programs.length === 0 && <p className="openwith-none">{t("workbench.no_supported_program_found")}</p>}
          {programs.map((target) => (
            <button
              key={target.id}
              role="menuitem"
              data-action="workspace.open-with"
              data-target={root}
              data-value={target.id}
              disabled={!target.available}
              title={target.available ? undefined : blockedNote(target)}
              onClick={() => run(target)}
            >
              <Tile id={target.id} />
              <span>{t("common.open_with_name", { name: openWithName(target) })}</span>
              {!target.available && <small>{t("common.inside_a_project_folder")}</small>}
            </button>
          ))}
          {files && <>
            <hr />
            <button role="menuitem" data-action="workspace.open-with" data-target={root} data-value={files.id} onClick={() => run(files)}>
              <StudioIcon name="folder" /><span>{revealWord(platform)}</span>
            </button>
          </>}
          <div className="openwith-path" title={root}><bdi dir="ltr">{root}</bdi></div>
        </div>
      )}
    </div>
  );
}
