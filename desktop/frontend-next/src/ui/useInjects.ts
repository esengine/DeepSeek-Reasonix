import { useEffect, useRef, type Dispatch, type MutableRefObject, type SetStateAction } from "react";
import type { PickedSnapshot } from "./composerChips";

export interface Injects {
  n: number;
  text?: string;
  session?: { id: string; title: string };
  element?: PickedSnapshot;
  elements?: PickedSnapshot[];
  files?: File[];
  pick?: { elements: PickedSnapshot[]; files?: File[] };
}

// The composer's one-way door from the window into the box: a session, a picked
// element (or a set), a file, or text. Kept out of Composer.tsx so that file
// stays inside its size budget.
export function useInjects(args: {
  inject?: Injects;
  caret: number;
  pending: MutableRefObject<number | [number, number] | null>;
  setText: Dispatch<SetStateAction<string>>;
  addSession: (s: { id: string; title: string }) => void;
  addElement: (p: PickedSnapshot) => void;
  attach: (files: File[]) => void;
  focus: () => void;
}) {
  const seen = useRef(args.inject?.n ?? 0);
  const { inject, caret, pending, setText, addSession, addElement, attach, focus } = args;
  useEffect(() => {
    if (!inject?.n || inject.n === seen.current) return;
    seen.current = inject.n;
    if (inject.session) {
      addSession(inject.session);
      queueMicrotask(focus);
      return;
    }
    // A pick is one act: its elements and their picture arrive together, so a
    // later text or element inject can never replay the previous pick.
    if (inject.pick) {
      for (const one of inject.pick.elements) addElement(one);
      if (inject.pick.files?.length) attach(inject.pick.files);
      queueMicrotask(focus);
      return;
    }
    if (inject.element) {
      addElement(inject.element);
      queueMicrotask(focus);
      return;
    }
    if (inject.elements?.length) {
      for (const one of inject.elements) addElement(one);
      queueMicrotask(focus);
      return;
    }
    if (inject.files?.length) {
      attach(inject.files);
      queueMicrotask(focus);
      return;
    }
    const where = caret;
    setText((prev) => {
      const at = Math.min(where, prev.length);
      const lead = at > 0 && !/\s$/.test(prev.slice(0, at)) ? " " : "";
      const piece = lead + (inject.text ?? "") + " ";
      pending.current = at + piece.length;
      return prev.slice(0, at) + piece + prev.slice(at).replace(/^ /, "");
    });
    queueMicrotask(focus);
  }, [inject?.n]);
}
