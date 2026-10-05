import type { KeyboardEvent } from "react";
import type { HubPort } from "../port/hub";
import { t } from "../i18n";
import { StudioIcon } from "./StudioIcon";

export function WorkspaceOrder({ root, position, total, hub, reload, dismiss, onError }: {
  root: string;
  position: number;
  total: number;
  hub: HubPort;
  reload: () => Promise<void>;
  dismiss: () => void;
  onError: (e: unknown) => void;
}) {
  const move = async (direction: -1 | 1) => {
    dismiss();
    try {
      await hub.moveWorkspace(root, direction);
      await reload();
    } catch (e) {
      onError(e);
    }
  };
  return (
    <div className="session-pop-group">
      {([-1, 1] as const).map((direction) => (
        <button key={direction} role="menuitem" data-action="workspace.move" data-target={root} data-value={direction}
          disabled={position < 0 || position + direction < 0 || position + direction >= total}
          onClick={() => void move(direction)}>
          <StudioIcon name="arrow" data-flip={direction === 1 ? "" : undefined} />
          <span>{t(direction === -1 ? "上移" : "下移")}</span>
        </button>
      ))}
    </div>
  );
}

export function workspaceMenuKeys(ev: KeyboardEvent<HTMLDivElement>) {
  if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(ev.key)) return;
  ev.preventDefault();
  ev.stopPropagation();
  const items = Array.from(ev.currentTarget.querySelectorAll<HTMLButtonElement>('button[role="menuitem"]:not(:disabled)'));
  if (!items.length) return;
  const at = items.indexOf(document.activeElement as HTMLButtonElement);
  const to = ev.key === "Home" ? 0 : ev.key === "End" ? items.length - 1 : (at + (ev.key === "ArrowDown" ? 1 : -1) + items.length) % items.length;
  items[to].focus();
}
