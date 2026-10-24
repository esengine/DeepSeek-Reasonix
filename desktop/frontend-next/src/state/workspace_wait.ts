import type { WireEvent } from "../port/wire";
import type { Item, SessionState } from "./session_types";
import { RUNNING } from "./chip";

const WAITING_WORKSPACE = "等待工作区";
const opening = "workspace_lease";
const resumed = "workspace_lease_resumed";
const abandoned = "workspace_lease_abandoned";

export function reduceWorkspaceWait(s: SessionState, ev: WireEvent, nextId: () => string): SessionState | undefined {
  if (ev.code !== opening && ev.code !== resumed && ev.code !== abandoned) return undefined;
  const id = ev.workspaceLease?.transactionId || undefined;
  const items = s.items.slice();
  let index = -1;
  for (let i = items.length - 1; i >= 0; i--) {
    const it = items[i];
    if (it.t === "notice"
      && (it.code === opening || (id && (it.code === resumed || it.code === abandoned)))
      && (it.workspaceLease?.transactionId || undefined) === id) {
      index = i;
      break;
    }
  }
  const previous = index >= 0 ? items[index] : undefined;
  if (id && previous?.t === "notice" && previous.code !== opening) return s;
  const fresh: Item = {
    t: "notice", id: previous?.id ?? nextId(), level: ev.level ?? "info",
    text: ev.text ?? "", detail: ev.detail, code: ev.code, workspaceLease: ev.workspaceLease,
  };
  if (index >= 0 && (id || ev.code !== opening)) items[index] = fresh;
  else items.push(fresh);
  const waiting = items.some((it) => it.t === "notice" && it.code === opening);
  return { ...s, items, doing: waiting ? WAITING_WORKSPACE : s.doing === WAITING_WORKSPACE ? RUNNING : s.doing };
}
