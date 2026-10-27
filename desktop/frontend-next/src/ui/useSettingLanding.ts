import { useEffect, type Dispatch, type RefObject, type SetStateAction } from "react";

export function useSettingLanding(root: RefObject<HTMLDivElement | null>, anchor: string, section: string, setAnchor: Dispatch<SetStateAction<string>>) {
  useEffect(() => {
    if (!anchor) return;
    const col = root.current?.querySelector(".prefs-col");
    if (!col) return;
    let target: HTMLElement | null = null;
    let done: ReturnType<typeof setTimeout> | undefined;
    const behavior = matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth";
    const aim = () => {
      const found = col.querySelector<HTMLElement>(`#set-${CSS.escape(anchor)}`);
      if (!found) return;
      for (let parent = found.closest("details"); parent; parent = parent.parentElement?.closest("details") ?? null)
        parent.open = true;
      if (target !== found) {
        target?.removeAttribute("data-landed");
        target = found;
        target.setAttribute("data-landed", "");
        clearTimeout(done);
        done = setTimeout(() => setAnchor(""), 900);
      }
      found.scrollIntoView({ block: "start", behavior });
    };
    const grew = new ResizeObserver(aim);
    const arrived = new MutationObserver(aim);
    grew.observe(col);
    arrived.observe(col, { childList: true, subtree: true });
    aim();
    return () => {
      clearTimeout(done);
      grew.disconnect();
      arrived.disconnect();
      target?.removeAttribute("data-landed");
    };
  }, [root, anchor, section, setAnchor]);
}
