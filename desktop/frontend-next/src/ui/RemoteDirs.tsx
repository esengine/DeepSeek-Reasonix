import { useCallback, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { HubPort } from "../port/hub";
import type { RemoteListing } from "../port/remote";

interface Props {
  hub: HubPort;
  host: string;
  // Where to open on, verbatim: the machine's own default beats its login home
  // as a first screen. Not its parent, however likely the sibling wanted is —
  // cutting a path this side does not spell is what "up" is for.
  start?: string;
  onPick: (path: string) => void;
  onClose: () => void;
}

// What the far machine answered, and nothing about asking it. A listing is
// dimmed while the next one is on its way rather than cleared: emptying the
// rows takes away the only account of where the reader currently is.
export function DirRows({
  listing,
  busy,
  onGo,
}: {
  listing: RemoteListing | null;
  busy: boolean;
  onGo: (path: string) => void;
}) {
  return (
    <>
      <div className="picklist" aria-label={t("子目录")} data-busy={busy ? "" : undefined}>
        {(listing?.folders ?? []).map((folder) => (
          <button key={folder.path} className="pickrow" onClick={() => onGo(folder.path)}>
            <svg viewBox="0 0 16 16" aria-hidden="true">
              <path d="M2.2 12.4V4.1h4l1.3 1.6h6.3v6.7z" />
            </svg>
            <span dir="ltr">{folder.name}</span>
          </button>
        ))}
        {/* The first answer may be a cold dial, and it may stop to ask for a
            host key on the way. A dimmed empty box says none of that. */}
        {!listing && busy ? <p className="pickempty">{t("正在连上去…")}</p> : null}
        {listing && !listing.folders.length && !busy ? (
          <p className="pickempty">{t("这个目录下面没有子目录了 —— 它自己就可以是工作区")}</p>
        ) : null}
      </div>
      {/* A cap that says nothing reads as "that folder is not there". */}
      {listing?.truncated ? (
        <p className="picknote">{t("这个目录太大，只列了前面一部分。要找的在后面的话，直接把路径打上去。")}</p>
      ) : null}
    </>
  );
}

// Picking a folder on another machine, before there is a kernel over there to
// ask: the link layer answers over the file protocol alone. The folder shown is
// the selection — a separately highlighted row would be a second way to say
// which one is meant, and the two disagree the moment a path is typed.
export function RemoteDirs({ hub, host, start, onPick, onClose }: Props) {
  const [listing, setListing] = useState<RemoteListing | null>(null);
  // What the path field holds, kept apart from where we are: typing is not
  // navigating, or every keystroke would be a round trip to another machine.
  const [draft, setDraft] = useState(start ?? "");
  const [busy, setBusy] = useState(true);
  const [err, setErr] = useState("");
  const box = useRef<HTMLInputElement>(null);
  const veiled = useRef(false);

  const go = useCallback(
    async (path: string) => {
      setBusy(true);
      setErr("");
      try {
        const next = await hub.remoteDirs(host, path);
        setListing(next);
        // The far machine resolved it — ~ is expanded over there, and a symlink
        // answers with what it points at. Showing what was typed instead would
        // put a path in the field that stepping up from leads somewhere else.
        setDraft(next.path);
      } catch (e) {
        // The listing on screen stays: a mistyped path is fixed by editing the
        // one in the field, and clearing the rows takes away what it was.
        setErr(reason(e));
      } finally {
        setBusy(false);
      }
    },
    [hub, host],
  );

  useEffect(() => {
    void go(start ?? "");
    box.current?.focus();
  }, [go, start]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    addEventListener("keydown", onKey);
    return () => removeEventListener("keydown", onKey);
  }, [onClose]);

  const here = listing?.path ?? "";
  // Through the body, because the settings sheet it can also be opened from
  // blurs its backdrop — and that makes a fixed child measure itself against
  // the sheet rather than the window.
  return createPortal(
    <div
      className="pickveil"
      role="dialog"
      aria-modal="true"
      aria-labelledby="pick-t"
      onMouseDown={(ev) => {
        veiled.current = ev.target === ev.currentTarget;
      }}
      onMouseUp={(ev) => {
        // Both ends on the veil, or a drag that started inside the card and
        // finished outside it would dismiss what it was selecting from.
        if (veiled.current && ev.target === ev.currentTarget) onClose();
      }}
    >
      <div className="pickcard">
        <h2 id="pick-t">{t("在 {host} 上选一个目录", { host })}</h2>

        <form
          className="pickpath"
          onSubmit={(ev) => {
            ev.preventDefault();
            void go(draft);
          }}
        >
          <button
            type="button"
            className="pickup"
            disabled={!listing?.parent || busy}
            title={t("上一级")}
            aria-label={t("上一级")}
            onClick={() => void go(listing?.parent ?? "")}
          >
            <svg viewBox="0 0 16 16" aria-hidden="true">
              <path d="M8 12.3V3.7M4.2 7.5 8 3.7l3.8 3.8" />
            </svg>
          </button>
          <input
            ref={box}
            value={draft}
            spellCheck={false}
            dir="ltr"
            placeholder={t("那台机器上的路径")}
            aria-label={t("那台机器上的路径")}
            onChange={(ev) => setDraft(ev.target.value)}
          />
          <button type="submit" className="pickgo" disabled={busy || !draft.trim()}>
            {t("转到")}
          </button>
        </form>

        {err ? <p className="pickerr">{err}</p> : null}

        <DirRows listing={listing} busy={busy} onGo={(dir) => void go(dir)} />

        <div className="pickact">
          <button onClick={onClose}>{t("取消")}</button>
          <button data-go="" disabled={!here || busy} data-action="remote.open" onClick={() => onPick(here)}>
            {t("就用这里")}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  );
}
