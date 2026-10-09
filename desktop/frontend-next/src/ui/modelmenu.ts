// modelmenu.ts — the model picker's rows, grouped the way the account is.
import type { ModelEntry } from "../port/port";
import type { MenuItem } from "./Menu";
import { groupVendors } from "./Models";
import { t } from "../i18n";
import { KIND_LABEL } from "./vendors";
import { orderAccounts } from "../state/providerorder";

// Every account gets a heading with its endpoint: the provider name is the
// config's word for an entry, not the user's for an endpoint, and two doors onto
// one account share it. Rows carry the wire format beside the model name.
export function modelMenu(models: ModelEntry[], order: readonly string[] = []): MenuItem[] {
  const accounts = orderAccounts(groupVendors(models), order);
  const out: MenuItem[] = [];
  for (const [i, a] of accounts.entries()) {
    const mono = monogram(a.label, a.key);
    out.push({ value: `__account:${a.key}`, label: a.label, right: a.host, header: true, divide: i > 0, mono });
    for (const kind of a.kinds) {
      for (const m of a.byKind[kind]) {
        out.push({
          value: m.ref,
          label: m.model,
          meta: t(KIND_LABEL[kind] ?? kind),
          badge: m.vision ? t("读图") : undefined,
          mono,
        });
      }
    }
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
// same letter and tone, and no vendor artwork is bundled.
export function monogram(label: string, key: string): { letter: string; tone: number } {
  const letter = [...label.trim()].find((c) => /[\p{L}\p{N}]/u.test(c))?.toUpperCase() ?? "?";
  let h = 0;
  for (const c of key) h = (h * 31 + c.charCodeAt(0)) >>> 0;
  return { letter, tone: h % TONES };
}
