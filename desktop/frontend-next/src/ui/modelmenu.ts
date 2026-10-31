// modelmenu.ts — the model picker's rows, grouped the way the account is.
import type { ModelEntry } from "../port/port";
import type { MenuItem } from "./Menu";
import { groupVendors } from "./Models";
import { t } from "../i18n";
import { UNCONFIGURED_REF } from "./ConnectNote";
import { KIND_LABEL } from "./vendors";
import { orderAccounts } from "../state/providerorder";
import { modelHealthStatus, type ModelHealth } from "./modelhealth";

// modelTooltip names the active model with the picker's canonical account label.
export function modelTooltip(models: ModelEntry[], current: string | undefined, label: string): string {
  const account = groupVendors(models).find((a) =>
    a.kinds.some((kind) => a.byKind[kind].some((model) => model.ref === current)));
  return account ? `${account.label} / ${label}` : current ?? label;
}

// modelLabel is how the composer names the session's model: the alias, else the
// entry's short form, else the id without its provider.
export function modelLabel(models: ModelEntry[], status: { modelRef?: string; label?: string } | null | undefined): string {
  if (status?.modelRef === UNCONFIGURED_REF) return t("未连接");
  const m = models.find((x) => x.ref === status?.modelRef);
  return m?.modelAlias?.trim() || m?.modelShort || (status?.modelRef?.replace(/^[^/]+\//, "") ?? status?.label ?? "—");
}

// Every account gets a heading with its endpoint: the provider name is the
// config's word for an entry, not the user's for an endpoint, and two doors onto
// one account share it. Rows carry the wire format beside the model name.
export function modelMenu(models: ModelEntry[], order: readonly string[] = [], current?: string, health: ModelHealth = {}): MenuItem[] {
  const accounts = orderAccounts(groupVendors(models), order);
  const out: MenuItem[] = [];
  for (const [i, a] of accounts.entries()) {
    const mono = monogram(a.label, a.key);
    const mark = a.kinds.flatMap((k) => a.byKind[k]).map((m) => m.serviceMark ?? m.mark).find(Boolean);
    out.push({ value: `__account:${a.key}`, label: a.label, right: a.host, header: true, divide: i > 0, mono, mark });
    for (const kind of a.kinds) {
      for (const m of a.byKind[kind]) {
        out.push({
          value: m.ref,
          label: m.modelAlias?.trim() || m.modelShort || m.model,
          desc: m.modelAlias?.trim() ? m.model : undefined,
          status: modelHealthStatus(health[m.provider]),
          meta: t(KIND_LABEL[kind] ?? kind),
          badge: m.vision ? t("读图") : undefined,
          mono,
          mark: m.mark,
        });
      }
    }
  }
  const held = out.find((item) => item.value === current);
  if (held) {
    out.splice(out.indexOf(held), 1);
    for (let i = out.length - 1; i >= 0; i--) {
      if (out[i].header && (!out[i + 1] || out[i + 1].header)) out.splice(i, 1);
    }
    for (const item of out) if (item.header) item.divide = true;
    out.unshift(held);
  }
  out.push({
    value: "__manage-models",
    label: t("管理模型与连接"),
    desc: t("服务商、API 与可用模型"),
    plain: true,
    divide: out.length > 0,
  });
  return out;
}

const TONES = 5;

// A letter chip stands in for a vendor mark: the same provider always draws the
// same letter and tone, where no declared vendor has artwork.
export function monogram(label: string, key: string): { letter: string; tone: number } {
  const letter = [...label.trim()].find((c) => /[\p{L}\p{N}]/u.test(c))?.toUpperCase() ?? "?";
  let h = 0;
  for (const c of key) h = (h * 31 + c.charCodeAt(0)) >>> 0;
  return { letter, tone: h % TONES };
}

/** The headings a menu offers to narrow by, in the order they appear. */
export function groupHeadings(items: MenuItem[]): MenuItem[] {
  return items.filter((it) => it.header);
}

/** The remembered choices that are still on offer. A choice for an account no
 *  longer listed must not empty the menu, so it simply stops counting. */
export function chosenGroups(items: MenuItem[], selected: readonly string[]): string[] {
  const offered = new Set(groupHeadings(items).map((h) => h.value));
  return selected.filter((key) => offered.has(key));
}

/** The rows of the chosen groups. What belongs to no group (the current model,
 *  pinned before the first heading) and every action stay. */
export function narrowToGroups(items: MenuItem[], chosen: readonly string[]): MenuItem[] {
  if (chosen.length === 0) return items;
  let inGroup = false;
  let keep = true;
  return items.filter((it) => {
    if (it.header) {
      inGroup = true;
      keep = chosen.includes(it.value);
      return keep;
    }
    if (it.plain) return true;
    return inGroup ? keep : true;
  });
}
