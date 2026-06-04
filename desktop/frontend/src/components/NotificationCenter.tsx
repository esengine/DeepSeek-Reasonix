import { useEffect, useState } from "react";
import { Bell, X } from "lucide-react";
import { clear, markAllRead, snapshot, subscribe, type Notification, unread } from "../lib/notificationCenter";

// NotificationCenter is the in-app drawer of recent notifications.
// The trigger chip sits in the topbar (a small Bell icon) and shows
// an unread dot when unread() > 0. Opening the drawer calls
// markAllRead() and renders the snapshot in reverse-chronological
// order. Each entry is dismissable; a "Clear all" link at the foot
// empties the store.
//
// The list is reactive: subscribe() is called once on mount, and
// the local copy is replaced on every push. The drawer doesn't poll.
//
// We use real relative time formatting ('3m ago', '2h ago') without
// a date-fns dep — the formatter is a 12-line helper that handles
// the only two cases that matter in this UI (sub-minute and sub-day).
export function NotificationCenter({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const [list, setList] = useState<Notification[]>(snapshot());
  useEffect(() => subscribe(setList), []);
  // Mark all read on open; the unread dot in the trigger chip
  // disappears immediately.
  useEffect(() => {
    if (open) markAllRead();
  }, [open]);

  if (!open) return null;
  return (
    <div className="drawer-backdrop" onClick={onClose} role="presentation">
      <aside className="drawer" role="dialog" aria-modal="true" aria-label="Notifications" onClick={(e) => e.stopPropagation()}>
        <header className="drawer__head">
          <div className="drawer__title">Notifications</div>
          <span className="drawer__spacer" />
          {list.length > 0 && (
            <button type="button" className="chip" onClick={() => clear()}>
              Clear all
            </button>
          )}
          <button type="button" className="chip" onClick={onClose} aria-label="Close notifications">
            <X size={13} />
          </button>
        </header>
        <div className="drawer__body notif">
          {list.length === 0 ? (
            <div className="notif__empty">No notifications yet.</div>
          ) : (
            <ul className="notif__list">
              {list.map((n) => (
                <li key={n.id} className={`notif__item notif__item--${n.level}`}>
                  <div className="notif__head">
                    <Bell size={11} aria-hidden="true" />
                    <span className="notif__title">{n.title}</span>
                    {n.source && <span className="notif__source">{n.source}</span>}
                    <span className="notif__spacer" />
                    <span className="notif__ago">{formatAgo(n.at)}</span>
                  </div>
                  {n.body && <div className="notif__body">{n.body}</div>}
                </li>
              ))}
            </ul>
          )}
        </div>
      </aside>
    </div>
  );
}

// NotificationBell is the topbar trigger chip. It shows an unread
// dot when unread() > 0. Subscribes to the store so the dot updates
// without the parent re-rendering.
export function NotificationBell({ onClick }: { onClick: () => void }) {
  const [n, setN] = useState<number>(unread());
  useEffect(() => subscribe(() => setN(unread())), []);
  return (
    <button
      type="button"
      className="chip chip--icon notif-bell"
      onClick={onClick}
      title="Notifications"
      aria-label={`Notifications (${n} unread)`}
    >
      <Bell size={13} />
      {n > 0 && <span className="notif-bell__dot" aria-hidden="true" />}
    </button>
  );
}

function formatAgo(at: number): string {
  const ms = Date.now() - at;
  if (ms < 60_000) return "just now";
  const m = Math.floor(ms / 60_000);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  const d = Math.floor(h / 24);
  return `${d}d ago`;
}
