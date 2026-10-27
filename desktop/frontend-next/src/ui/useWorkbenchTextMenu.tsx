import { useCallback, useEffect, useMemo, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type MouseEvent as ReactMouseEvent } from "react";
import { host, type TextContextMenuRequest } from "../port/host";
import { reason } from "../i18n/kernel";
import { WorkbenchSelectionMenu } from "./WorkbenchSelectionMenu";
import { readerTextSession, textMenuKey, type OfferTextMenu, type TextMenuAction, type TextMenuSession } from "./workbench_text_menu";

type Request = {
  owner: object;
  session: TextMenuSession;
  path: string;
  native: TextContextMenuRequest;
};

export function useWorkbenchTextMenu({ path, mode, scope, shown, onQuote, onError }: {
  path: string; mode: string; scope: object; shown: boolean;
  onQuote?: (text: string, path: string) => void;
  onError: (error: string) => void;
}) {
  const owner = useMemo(() => ({ path, mode, scope, shown }), [path, mode, scope, shown]);
  const currentOwner = useRef(owner);
  currentOwner.current = owner;
  const callbacks = useRef({ onQuote, onError });
  callbacks.current = { onQuote, onError };
  const pending = useRef<Request | null>(null);
  const [menu, setMenu] = useState<Request | null>(null);
  useEffect(() => {
    setMenu(null);
    return () => { if (pending.current?.owner === owner) pending.current = null; };
  }, [owner]);
  const close = useCallback((restore: boolean) => {
    const request = pending.current;
    pending.current = null;
    setMenu(null);
    if (restore && request?.owner === currentOwner.current) request.session.focus();
  }, []);
  const offer: OfferTextMenu = (event, session) => {
    if (!shown || !path || (!session.editable && !session.selected)) return;
    event.preventDefault();
    event.stopPropagation();
    session.focus();
    const pointed = "clientX" in event && (event.clientX !== 0 || event.clientY !== 0);
    const rect = session.anchor.getBoundingClientRect();
    const request: Request = {
      owner, session, path,
      native: {
        editable: session.editable, selected: session.selected, canUndo: session.canUndo, canRedo: session.canRedo,
        quote: !!callbacks.current.onQuote && !!session.text.trim(),
        x: pointed ? event.clientX : rect.left + 24, y: pointed ? event.clientY : rect.top + 24,
      },
    };
    pending.current = request;
    setMenu(null);
    const current = () => pending.current === request && currentOwner.current === owner && session.valid();
    void host().showTextContextMenu(request.native).then((result) => {
      if (!current()) return;
      if (result === null) { setMenu(request); return; }
      if (result === "") {
        pending.current = null;
        session.focus(false);
      } else void action(result);
    }).catch((error) => {
      if (!current()) return;
      pending.current = null;
      session.focus();
      callbacks.current.onError(reason(error));
    });
  };
  const reader = (event: ReactMouseEvent<HTMLElement> | ReactKeyboardEvent<HTMLElement>) => {
    if ("key" in event && !textMenuKey(event.nativeEvent)) return;
    if (!(event.target instanceof Element) || event.target.closest("input, textarea, button, select, [contenteditable=true]")) return;
    const surface = event.target.closest<HTMLElement>(".workbench-read, .workbench-diff, .document-table, .textLayer");
    if (!surface || !event.currentTarget.contains(surface)) return;
    const session = readerTextSession(surface);
    if (session) offer(event.nativeEvent, session);
  };
  const action = async (command: TextMenuAction | "quote") => {
    const request = pending.current;
    if (!request || request.owner !== currentOwner.current || !request.session.valid()) { close(false); return; }
    setMenu(null);
    const current = () => pending.current === request && request.owner === currentOwner.current && request.session.valid();
    try {
      if (command === "quote") {
        request.session.focus();
        if (request.native.quote) callbacks.current.onQuote?.(request.session.text, request.path);
      } else await request.session.run(command, current);
      if (pending.current === request && request.owner === currentOwner.current) request.session.focus(false);
    } catch (error) {
      if (current()) { request.session.focus(); callbacks.current.onError(reason(error)); }
    } finally {
      if (pending.current === request) pending.current = null;
    }
  };
  return {
    offer,
    readerProps: { onContextMenu: reader, onKeyDown: reader },
    menu: menu?.owner === owner ? <WorkbenchSelectionMenu menu={menu.native} onAction={action} onClose={close} /> : null,
  };
}
