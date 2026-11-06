import { t } from "../i18n";
import { SETTING_AT, type SettingScope } from "./prefsnav";
import type { ReactNode } from "react";
import { SettingDeep } from "./SettingDeep";

// id is required, and that is the whole enforcement: a settings block cannot
// be written without naming itself, and a name the catalogue does not carry
// fails the gate. So "which setting is this, and when does changing it take
// effect" has no default and no way to be left unanswered.
export function Group({
  id, title, hint, now, action, pill, inline, children,
}: {
  id: string; title: string; hint?: string; now?: string; action?: ReactNode; pill?: string; inline?: boolean; children?: ReactNode;
}) {
  return (
    <SettingDeep id={id} title={title} now={now} hint={hint} act={pill}>
    <section className="grp" id={`set-${id}`} data-setting={id} data-inline={inline ? "" : undefined}>
      <div className="grp-hd">
        <h3>{title}</h3>
        <ScopeTag id={id} />
        {now && <span className="now">{now}</span>}
        {action}
      </div>
      {hint && <p className="hint" title={hint}>{hint}</p>}
      {children && <div className="grp-items">{children}</div>}
      {/* A fact about this block, at the weight of a fact: what it costs to
          change is not a warning, and three of them on a page should not read
          as three alarms. */}
      <ApplyNote id={id} />
    </section>
    </SettingDeep>
  );
}

/** Who a change reaches and when it is in force. Separate from Group so a block
 *  that draws its own frame still cannot go without one — the appearance page
 *  builds its sections by hand, and the questions are the same there.
 *
 *  Both halves come from the catalogue and neither is written at a call site:
 *  one table answers for the block and for the search result that leads to it,
 *  so the two can never disagree about what a setting owns. */
export function ApplyNote({ id }: { id: string }) {
  const entry = SETTING_AT(id);
  if (!entry) return null;
  const apply = entry.apply === "none" ? null : t(APPLY_SAID[entry.apply]);
  // One text node: .apply is a flex row, and separate children would take the
  // row's gap on both sides of the separator.
  return (
    <p className="apply" data-apply={entry.apply} data-scope={entry.scope} data-usual={isUsual(entry) ? "" : undefined}>
      {[t(SCOPE_SAID[entry.scope]), apply].filter(Boolean).join(" · ")}
    </p>
  );
}

// This machine, in force at once, is what nearly every setting is, so it is
// said once at the top of the page; only a block that differs says so.
const isUsual = (entry: { scope: SettingScope; apply: string }) =>
  entry.scope === "machine" && (entry.apply === "immediate" || entry.apply === "none");

/** What a block does differently from the page's usual, beside its name. */
export function ScopeTag({ id }: { id: string }) {
  const entry = SETTING_AT(id);
  if (!entry || isUsual(entry)) return null;
  const said = [entry.scope === "machine" ? null : t(SCOPE_SAID[entry.scope]), entry.apply === "immediate" || entry.apply === "none" ? null : t(APPLY_SAID[entry.apply])];
  return <span className="scope-tag">{said.filter(Boolean).join(" · ")}</span>;
}

/** Whose setting this is. A fact, at the weight of a fact — no accent, no chip:
 *  ownership is not a warning, and thirty of them down a page must not read as
 *  thirty alarms. */
export const SCOPE_SAID: Record<SettingScope, string> = {
  session: "common.this_session",
  model: "settings.this_model",
  workspace: "settings.this_workspace",
  machine: "session.this_machine",
  account: "settings.account",
  chosen: "settings.you_choose_where",
};

// 「已保存」和「生效」是两件事，重启那一档必须把它们分开说 —— 否则关掉设置的人
// 不知道刚填的东西还在不在。
const APPLY_SAID: Record<"immediate" | "runtime-rebuild" | "restart" | "on-open" | "next-turn" | "next-rebuild", string> = {
  immediate: "look.in_effect_now",
  "runtime-rebuild": "settings.rebuilds_the_runtime",
  restart: "settings.takes_effect_after_a_restart",
  "on-open": "settings.applies_to_new_sessions",
  "next-turn": "settings.applies_from_the_next_turn",
  "next-rebuild": "settings.applies_at_the_next_session",
};
