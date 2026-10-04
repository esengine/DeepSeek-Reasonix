import { useCallback, useEffect, useMemo, useState } from "react";
import { adopt as adoptLang } from "../i18n";
import type { Appearance as Look, ThemePack } from "../port/port";
import type { HubPort, RuntimeView } from "../port/hub";
import { apply as applyLook } from "./look";
import { apply as applyThemePack } from "./theme";
import { trailing } from "./trailing";

export interface Paint {
  theme: string;
  setTheme: (v: string) => void;
  // What "auto" resolved to. Panes take it as a prop, not off the root.
  scheme: "light" | "dark";
  contrast: string;
  setContrast: (v: string) => void;
  weight: string;
  setWeight: (v: string) => void;
  look: Look;
  onLook: (next: Look) => void;
  pack: ThemePack | null;
  reloadThemes: () => void;
}

/** How this window is painted: the scheme it resolved to, the pack behind it,
 *  and the reader's own size and contrast. One concern, and the only one that
 *  writes `documentElement`'s dataset, so nothing else has to know the order
 *  a pack and a look have to be applied in. */
export function usePaint(hub: HubPort, runtimes: RuntimeView[], running: boolean, onError: (e: unknown) => void): Paint {
  const [theme, setTheme] = useState(() => localStorage.getItem("rx-theme") ?? "auto");
  const [scheme, setScheme] = useState<"light" | "dark">("dark");
  // "" means never chosen, which is what lets the system's own contrast setting
  // decide. Any explicit pick wins over it from then on.
  const [contrast, setContrast] = useState(() => localStorage.getItem("rx-contrast") ?? "");
  // 空串是「跟随语言」：中文界面本来就该比西文粗一档，样式表按 :lang 给默认。
  const [weight, setWeight] = useState(() => localStorage.getItem("rx-weight") ?? "");
  const [pack, setPack] = useState<ThemePack | null>(null);
  const [look, setLook] = useState<Look>({});

  // Appearance is the window's, not the focused pane's. A remote kernel keeps
  // its own copy, and adopting that one repaints this window because the
  // reader changed tabs — so these three read and write the local pane only.
  const lookPort = useMemo(() => {
    const local = runtimes.find((rt) => !rt.host);
    return local ? hub.portFor(local) : null;
  }, [hub, runtimes]);

  const reloadThemes = useCallback(() => {
    lookPort
      ?.themes()
      .then((list) => setPack(list.find((p) => p.active) ?? null))
      .catch(onError);
  }, [lookPort, onError]);
  useEffect(reloadThemes, [reloadThemes]);

  useEffect(() => {
    lookPort
      ?.appearance()
      .then((next) => {
        // The kernel's copy is the authority across machines; adopt reloads if
        // this window booted in the wrong language from a stale local cache.
        if (adoptLang(next.language)) return;
        setLook(next);
      })
      .catch(() => {});
  }, [lookPort]);

  // The control moves now and the config catches up: a size or a colour that
  // waits on a round trip reads as a dead click. The kernel's answer is still
  // what is kept, since it clamps what the slider sent.
  const saveLook = useMemo(
    () => (lookPort ? trailing((next: Look) => lookPort.saveAppearance(next), setLook, onError) : null),
    [lookPort, onError],
  );
  const onLook = useCallback(
    (next: Look) => {
      setLook(next);
      saveLook?.(next);
    },
    [saveLook],
  );

  // A pack carries a light and a dark set, so it is repainted with the scheme
  // rather than once at load: switching the OS to dark has to move both. The
  // running flag rides along because a pack's picture recedes while a turn is
  // in flight — the transition is in CSS, this only moves the target.
  useEffect(() => {
    const mq = matchMedia("(prefers-color-scheme: dark)");
    const paint = () => {
      const now = theme === "auto" ? (mq.matches ? "dark" : "light") : theme;
      document.documentElement.dataset.theme = now;
      setScheme(now as "light" | "dark");
      // The active pack is the same state shown by the appearance picker.
      // Applying `null` here made activation persist in the kernel while the
      // shell deliberately kept drawing the default palette, so a successful
      // click looked broken until forever. Keep the authored Studio layout,
      // but let the selected pack supply its documented colour/background
      // tokens immediately.
      applyThemePack(pack, now as "light" | "dark", running, contrast);
      // After the pack, never before: size and type are the reader's, and a
      // palette someone else authored does not get to overrule them.
      applyLook(look, running);
    };
    paint();
    mq.addEventListener("change", paint);
    localStorage.setItem("rx-theme", theme);
    return () => mq.removeEventListener("change", paint);
  }, [theme, pack, running, look, contrast]);

  useEffect(() => {
    if (contrast) document.documentElement.dataset.contrast = contrast;
    else delete document.documentElement.dataset.contrast;
    localStorage.setItem("rx-contrast", contrast);
    if (weight) document.documentElement.dataset.weight = weight;
    else delete document.documentElement.dataset.weight;
    localStorage.setItem("rx-weight", weight);
  }, [contrast, weight]);

  return { theme, setTheme, scheme, contrast, setContrast, weight, setWeight, look, onLook, pack, reloadThemes };
}
