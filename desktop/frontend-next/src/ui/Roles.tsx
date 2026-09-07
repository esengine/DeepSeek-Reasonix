import { useRef, useState } from "react";
import { t } from "../i18n";
import type { ModelEntry, RoleAssignments } from "../port/port";
import { useDismiss } from "./dismiss";

// Five jobs, one default. Rendering them as five equal dropdowns would only
// move the confusion from the model list to a role list, so the main model is
// the anchor and a role draws a branch off it only once it stops following.

type RoleKey = keyof RoleAssignments;

const ROLES: [RoleKey, string, string][] = [
  ["planner", "计划", "只读地出计划"],
  ["subagent", "子代理", "派出去的活"],
  ["vision", "看图", "读主模型看不了的图"],
  ["guardian", "复核", "独立审这一轮"],
];

interface Props {
  models: ModelEntry[];
  roles: RoleAssignments | null;
  main?: string;
  busy: string;
  onSet: (role: string, ref: string) => void;
}

export function Roles({ models, roles, main, busy, onSet }: Props) {
  const [open, setOpen] = useState<RoleKey | null>(null);
  const anchor = models.find((m) => m.ref === main);
  // Whichever model an attachment actually reaches: the vision role if one is
  // assigned, otherwise the sub-agent it would be handed to, otherwise the main
  // model. The note below reports whether that model reads images at all.
  const visionRef = roles?.vision || roles?.subagent || main;
  const visionModel = models.find((m) => m.ref === visionRef);
  const readable = visionModel?.vision === true;

  if (!roles) return <div className="empty">{t("读不到分工。")}</div>;

  const following = ROLES.filter(([k]) => !roles[k]).length;

  return (
    <>
      <div className="band">
        <div className="anchor">
          <span className="cap">{t("对话 · 主模型")}</span>
          <span className="nm">{anchor?.model ?? main ?? "—"}</span>
          <span className="meta">
            {[anchor?.provider, following === ROLES.length ? t("所有分工都跟着它") : t("{n} 个分工跟着它", { n: following })]
              .filter(Boolean)
              .join(" · ")}
          </span>
        </div>
        <div className="fan">
          {ROLES.map(([key, name, tag]) => (
            <Slot
              key={key}
              name={t(name)}
              tag={t(tag)}
              set={roles[key]}
              models={models}
              busy={busy}
              open={open === key}
              onOpen={() => setOpen(open === key ? null : key)}
              onPick={(ref) => {
                setOpen(null);
                onSet(key, ref);
              }}
            />
          ))}
        </div>
      </div>
      <p className="note">
        {readable
          ? `主模型看不了的图会交给 ${visionModel?.model}，它读图，所以附件真的会被看到。`
          : t("主模型看不了的图现在没人读得了 —— 会在发出去之前被丢掉。给「看图」指一个带「读图」标签的模型就能接上。")}
      </p>
    </>
  );
}

function Slot({
  name, tag, set, models, busy, open, onOpen, onPick,
}: {
  name: string; tag: string; set: string; models: ModelEntry[]; busy: string;
  open: boolean; onOpen: () => void; onPick: (ref: string) => void;
}) {
  const box = useRef<HTMLDivElement>(null);
  const chosen = models.find((m) => m.ref === set);
  useDismiss(open, box, onOpen);

  return (
    <div className="slotbox" ref={box}>
      <button
        className="slot"
        data-set={set ? "" : undefined}
        aria-expanded={open}
        aria-haspopup="listbox"
        disabled={busy !== ""}
        onClick={onOpen}
      >
        <i className="node" />
        <span className="role">{name}</span>
        {/* Keyed on the assignment so the value replays its entrance when it
            changes — the row is the only thing that moved, and a silent swap
            reads as nothing having happened. */}
        <span className="val" key={set || "follow"}>{set ? (chosen?.model ?? set) : t("跟随主模型")}</span>
        <span className="tag">{set ? t("已指派") : tag}</span>
      </button>
      {/* Kept conditional: .mgrp rounds its corners with overflow:hidden, released
          via :has(.rpick) only while the menu is open. Mounting it always would
          defeat that clip for good. */}
      {open && (
        <div className="rpick" role="listbox" aria-label={t("{name}用哪个模型", { name })}>
          <button role="option" data-action="roles.model" aria-selected={!set} data-cur={!set ? "" : undefined} onClick={() => onPick("")}>
            {t("跟随主模型")}
          </button>
          <div className="sep" />
          {models.map((m) => (
            <button
              key={m.ref}
              role="option"
                    data-action="roles.model"
              aria-selected={m.ref === set}
              data-cur={m.ref === set ? "" : undefined}
              onClick={() => onPick(m.ref)}
            >
              {m.model}
              <span className="sub">{m.vision ? "读图" : m.provider}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
