// modelmenu.ts — the model picker's rows, grouped the way the account is.
import type { ModelEntry } from "../port/port";
import type { MenuItem } from "./Menu";
import { groupVendors } from "./Models";
import { t } from "../i18n";
import { KIND_LABEL } from "./vendors";
import { orderAccounts } from "../state/providerorder";

// One flat list said "deepseek" four times: the provider name is the config's
// word for an entry, not the user's for an endpoint, and two doors onto one
// account share it. Every row keeps its source and wire format underneath the
// model name, so the second line is useful even when there is only one account.
export function modelMenu(models: ModelEntry[], order: readonly string[] = []): MenuItem[] {
  const accounts = orderAccounts(groupVendors(models), order);
  const out: MenuItem[] = [];
  for (const [i, a] of accounts.entries()) {
    const manyDoors = a.kinds.length > 1;
    const label = accounts.length > 1 || manyDoors;
    if (label) {
      out.push({ value: `__account:${a.key}`, label: a.label, right: a.host, header: true, divide: i > 0 });
    }
    for (const kind of a.kinds) {
      for (const m of a.byKind[kind]) {
        out.push({
          value: m.ref,
          label: m.model,
          desc: `${a.label} · ${t(KIND_LABEL[kind] ?? kind)}`,
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
