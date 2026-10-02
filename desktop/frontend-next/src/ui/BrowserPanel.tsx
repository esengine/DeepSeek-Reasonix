import { useEffect, useLayoutEffect, useMemo, useRef, useState, type FormEvent, type KeyboardEvent } from "react";
import { t } from "../i18n";
import type { AgentPort, BrowserTab } from "../port/port";
import { host, type BrowserControl, type BrowserLoadFailure, type ViewRect } from "../port/host";
import { SWAP_MARK } from "./swap";
import { occluded, visibleBox } from "./occlusion";
import { BrowserFailure } from "./BrowserFailure";

const START = "reasonix://start";

const sameBox = (a: ViewRect | null, b: ViewRect) =>
  !!a && a.x === b.x && a.y === b.y && a.width === b.width && a.height === b.height;

function normalise(value: string): string | null {
  const raw = value.trim();
  if (!raw) return START;
  if (/^reasonix:\/\//i.test(raw)) return START;
  const local = /^(?:localhost|127(?:\.\d+){3}|10(?:\.\d+){3}|192\.168(?:\.\d+){2}|172\.(?:1[6-9]|2\d|3[01])(?:\.\d+){2}|\[::1\]|[^./:]+)(?::\d+)?(?:\/|$)/i.test(raw);
  const candidate = local ? `http://${raw}` : /^[a-z][a-z0-9+.-]*:\/\//i.test(raw) ? raw : `https://${raw}`;
  try {
    const url = new URL(candidate);
    return url.protocol === "http:" || url.protocol === "https:" ? url.href : null;
  } catch {
    return null;
  }
}

function startPage(scheme: "light" | "dark"): string {
  const light = scheme === "light";
  const page = light ? "#f6f7f4" : "#171817";
  const text = light ? "#242622" : "#e9eae5";
  const muted = light ? "#747970" : "#90958d";
  const surface = light ? "#ffffff" : "#1c1d1b";
  const border = light ? "#dfe1db" : "#2c2f2b";
  return `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>
    :root{color-scheme:${scheme};font-family:Inter,"Noto Sans SC",system-ui,sans-serif;background:${page};color:${text}}
    *{box-sizing:border-box}body{margin:0;min-height:100vh;display:grid;place-items:center;padding:32px;background:${page}}
    main{width:min(520px,100%);text-align:center}.mark{width:42px;height:42px;margin:0 auto 18px;display:grid;place-items:center;border:1px solid ${border};border-radius:12px;background:${surface};color:#7188c4;font-size:22px}
    h1{margin:0 0 8px;font-size:18px;font-weight:620;letter-spacing:-.02em}p{margin:0;color:${muted};font-size:13px;line-height:1.7}
    .hint{margin-top:22px;padding:11px 14px;border:1px solid ${border};border-radius:9px;background:${surface};color:${muted};font-size:12px;text-align:left}
  </style></head><body><main><div class="mark">◎</div><h1>Reasonix Browser</h1><p>${t("在上方地址栏输入网址，即可在当前任务旁浏览资料。")}</p><div class="hint">${t("浏览内容不会替换或中断当前会话。部分网站禁止嵌入时，可使用右上角的外部打开。")}</div></main></body></html>`;
}

interface ManualProps {
  id: string;
  hidden?: boolean;
  // What the tab should call this one. Several browsers all named 「浏览器」
  // name none of them, and only this panel knows where it has got to.
  onAddress: (id: string, host: string) => void;
  onExternal: (url: string) => void;
  scheme: "light" | "dark";
}

export function ManualBrowserPanel({ id, hidden, onAddress, onExternal, scheme }: ManualProps) {
  const [history, setHistory] = useState([START]);
  const [index, setIndex] = useState(0);
  const [input, setInput] = useState(START);
  const [reload, setReload] = useState(0);
  const [invalid, setInvalid] = useState(false);
  const [loading, setLoading] = useState(false);
  const current = history[index] ?? START;
  const internal = current === START;
  const srcDoc = useMemo(() => startPage(scheme), [scheme]);
  useEffect(() => {
    onAddress(id, internal ? "" : new URL(current).hostname);
  }, [id, current, internal, onAddress]);

  const visit = (value: string) => {
    const next = normalise(value);
    if (!next) {
      setInvalid(true);
      return;
    }
    setInvalid(false);
    setLoading(next !== START);
    setInput(next);
    if (next === current) {
      setReload((value) => value + 1);
      return;
    }
    const nextHistory = history.slice(0, index + 1).concat(next);
    setHistory(nextHistory);
    setIndex(nextHistory.length - 1);
  };

  const move = (next: number) => {
    const value = history[next];
    if (!value) return;
    setIndex(next);
    setInput(value);
    setInvalid(false);
  };

  const submit = (event: FormEvent) => {
    event.preventDefault();
    visit(input);
  };

  // Some desktop webviews consume Enter before a form submit bubbles back to
  // React. Commit the address at the field as well; IME confirmation must not
  // navigate while the user is still composing Chinese text.
  const commitAddress = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key !== "Enter" || event.nativeEvent.isComposing) return;
    event.preventDefault();
    event.stopPropagation();
    visit(event.currentTarget.value);
  };

  return (
    <section className="studio-browser" hidden={hidden} aria-label={t("内置 Browser")}>
      <form className="studio-browser-toolbar" onSubmit={submit} data-action-submit="browser.navigate">
        <button type="button" data-action="browser.back" onClick={() => move(index - 1)} disabled={index === 0} aria-label={t("后退")} title={t("后退")}>
          <svg viewBox="0 0 16 16" aria-hidden="true"><path d="m9.5 3-5 5 5 5M5 8h7" /></svg>
        </button>
        <button type="button" data-action="browser.reload" onClick={() => setReload((value) => value + 1)} aria-label={t("刷新")} title={t("刷新")}>
          <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M12.7 6A5 5 0 1 0 13 9.5M12.7 6V2.8M12.7 6H9.5" /></svg>
        </button>
        <label className="studio-browser-address" data-invalid={invalid ? "" : undefined}>
          <svg viewBox="0 0 16 16" aria-hidden="true"><circle cx="8" cy="8" r="5.3" /><path d="M2.8 8h10.4M8 2.7c1.5 1.5 2.2 3.2 2.2 5.3S9.5 11.8 8 13.3C6.5 11.8 5.8 10.1 5.8 8S6.5 4.2 8 2.7Z" /></svg>
          <input data-action-change="browser.address" data-action-keydown="browser.navigate" value={input} onKeyDown={commitAddress} onChange={(event) => { setInput(event.target.value); setInvalid(false); }} aria-label={t("网页地址")} spellCheck={false} autoCapitalize="off" />
        </label>
        <button type="submit" className="studio-browser-go" aria-label={t("打开网页")}>{t("打开")}</button>
        <button type="button" data-action="browser.external" onClick={() => !internal && onExternal(current)} disabled={internal} aria-label={t("在外部浏览器打开")} title={t("在外部浏览器打开")}>
          <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M9 3h4v4M13 3 7.5 8.5" /><path d="M7 4H3v9h9V9" /></svg>
        </button>
      </form>
      {invalid && <p className="studio-browser-error" role="alert">{t("请输入有效的 http 或 https 地址")}</p>}
      <iframe
        key={`${current}:${reload}`}
        className="studio-browser-frame"
        title={t("Reasonix 内置 Browser")}
        src={internal ? undefined : current}
        srcDoc={internal ? srcDoc : undefined}
        sandbox="allow-scripts allow-forms allow-popups"
        referrerPolicy="no-referrer"
        onLoad={() => setLoading(false)}
        onError={() => setLoading(false)}
      />
      {/* Not a hint at an icon somewhere else: a site that refuses to be framed
          shows the shell's own "refused to connect", and the way out has to be
          beside it. */}
      {(loading || !internal) && (
        <footer className="studio-browser-note" data-loading={loading ? "" : undefined}>
          {loading && <span>{t("正在打开网页…")}</span>}
          {!internal && (
            <button type="button" data-action="browser.external" onClick={() => onExternal(current)}>
              {t("在外部浏览器打开")}
            </button>
          )}
        </footer>
      )}
    </section>
  );
}

// What useBrowserTabs holds before its first read, told apart by identity from a
// read that found no pages.
export const UNREAD_TABS: BrowserTab[] = [];

export function useBrowserTabs(port: AgentPort, moved: number): BrowserTab[] {
  const [tabs, setTabs] = useState<BrowserTab[]>(UNREAD_TABS);
  useEffect(() => {
    if (!host().drawsBrowserViews()) return;
    let live = true;
    port.browserTabs().then((next) => live && setTabs(next), () => {});
    return () => { live = false; };
  }, [port, moved]);
  return tabs;
}

export function AgentBrowserPanel({ tabs, shown, showTabs = true }: { tabs: BrowserTab[]; shown: boolean; showTabs?: boolean }) {
  const slot = useRef<HTMLDivElement>(null);
  const [picked, setPicked] = useState("");
  const current = tabs.find((tab) => tab.target === picked) ?? tabs.find((tab) => tab.active) ?? tabs[0];
  const [address, setAddress] = useState(current?.url ?? "");
  const [failures, setFailures] = useState<Record<string, BrowserLoadFailure>>({});
  const [refused, setRefused] = useState(false);
  const [frame, setFrame] = useState("");
  const target = current?.target ?? "";
  const failure = failures[target];

  useEffect(() => setAddress(current?.url ?? ""), [current?.url]);
  useEffect(
    () =>
      host().onBrowserLoadState(({ targetId, failure: next }) =>
        setFailures((prev) => {
          if (!next && !prev[targetId]) return prev;
          const out = { ...prev };
          if (next) out[targetId] = next;
          else delete out[targetId];
          return out;
        }),
      ),
    [],
  );
  // A page that did not load is put away, so the reason drawn in its slot is
  // not hidden under the native view. One the page covers is put away too, and
  // its last picture stands in the slot until nothing covers it any more.
  useLayoutEffect(() => {
    const el = slot.current;
    const shell = host();
    setFrame("");
    if (!el || !shown || !target || failure) {
      shell.hideBrowserView();
      return;
    }
    let raf = 0;
    let live = true;
    // Which freeze is current: a picture answering an older one is stale.
    let frozen = 0;
    let freezes = 0;
    // The rectangle the view was last placed at, so the hot path — the overlay
    // watchers fire on every streamed chunk — can tell "nothing moved" from a
    // layout worth a probe.
    let placed: ViewRect | null = null;
    // Set by the events that can change what covers the slot, as against the
    // ones that can only move the slot itself: the occlusion probe is real
    // work, and a stream needs it none of.
    let covers = true;
    const place = () => {
      raf = 0;
      const box = visibleBox(el);
      if (!box) {
        frozen = 0;
        placed = null;
        shell.hideBrowserView();
        return;
      }
      const moved = !sameBox(placed, box);
      placed = box;
      const swapping = document.documentElement.hasAttribute(SWAP_MARK);
      if (!moved && !covers && !swapping && !frozen) return;
      covers = false;
      if (swapping || occluded(el, box)) {
        if (frozen) return;
        const turn = (frozen = ++freezes);
        void shell.freezeBrowserView().then((picture) => {
          if (live && frozen === turn) setFrame(picture);
        });
        return;
      }
      if (frozen) setFrame("");
      frozen = 0;
      shell.showBrowserView(target, box);
    };
    // One placement per frame, not one per event and not one per 48ms tail:
    // through a drag, a window resize or a column's transition the slot moves
    // every frame, and a trailing timeout let the native view catch up in steps
    // — the page chasing its hole and jumping level at the end. The main side
    // drops a placement whose rectangle has not changed, so a quiet stream
    // costs no IPC at all.
    const schedule = () => { if (!raf) raf = requestAnimationFrame(place); };
    const scheduleCover = () => { covers = true; schedule(); };
    place();
    const resize = new ResizeObserver(schedule);
    resize.observe(el);
    const overlays = new MutationObserver(scheduleCover);
    overlays.observe(document.body, { childList: true, subtree: true, attributes: true });
    overlays.observe(document.documentElement, { attributes: true, attributeFilter: [SWAP_MARK, "style", "class"] });
    // A layer can leave without touching the DOM: a transition or an animation
    // ending, a hover, a scroll. The two events catch most of it; the poll is what
    // makes a page that is no longer covered come back whatever moved.
    document.addEventListener("transitionend", scheduleCover, true);
    document.addEventListener("animationend", scheduleCover, true);
    addEventListener("resize", schedule);
    const poll = window.setInterval(scheduleCover, 400);
    return () => {
      live = false;
      if (raf) cancelAnimationFrame(raf);
      clearInterval(poll);
      resize.disconnect();
      overlays.disconnect();
      document.removeEventListener("transitionend", scheduleCover, true);
      document.removeEventListener("animationend", scheduleCover, true);
      removeEventListener("resize", schedule);
      shell.hideBrowserView();
    };
  }, [shown, target, failure]);

  // The picture stands in for the page, so the page comes down only once it is
  // in the tree. Freezing used to take the view down itself, before the JPEG
  // had crossed back over the bridge — one frame of bare slot every time, and
  // the overlay that caused it a beat later with it.
  useLayoutEffect(() => {
    if (frame) host().hideBrowserView();
  }, [frame]);

  const control = (action: BrowserControl) => target && host().controlBrowserView(target, action);
  const open = (to: string) => {
    if (!target) return;
    void host()
      .navigateBrowserView(target, to)
      .then((ok) => setRefused(!ok));
  };
  const go = (event: FormEvent) => {
    event.preventDefault();
    open(address);
  };
  return (
    <div className="bpanel">
      <div className="bbar">
        {showTabs && <div className="vtabs" role="tablist">
          {tabs.map((tab) => (
            <button key={tab.target} className="tab btab" role="tab" data-action="browser.tab" data-target={tab.id} aria-selected={tab.target === target} title={tab.url} onClick={() => setPicked(tab.target)}>
              {tab.title || tab.url || t("空白页")}
            </button>
          ))}
        </div>}
        <div className="bnav">
          <button className="bbtn" data-action="browser.control" data-target={current?.id} data-value="back" aria-label={t("后退")} onClick={() => control("back")}>←</button>
          <button className="bbtn" data-action="browser.control" data-target={current?.id} data-value="forward" aria-label={t("前进")} onClick={() => control("forward")}>→</button>
          <button className="bbtn" data-action="browser.control" data-target={current?.id} data-value="reload" aria-label={t("重新加载")} onClick={() => control("reload")}>↻</button>
          <form className="baddr" data-action="browser.navigate" data-target={current?.id} onSubmit={go}>
            <input data-action="browser.navigate" data-target={current?.id} value={address} spellCheck={false} aria-label={t("网址")} onChange={(event) => { setAddress(event.target.value); setRefused(false); }} />
          </form>
        </div>
      </div>
      {refused && <p className="studio-browser-error" role="alert">{t("请输入有效的 http 或 https 地址")}</p>}
      <div className="bview" ref={slot}>
        {failure ? (
          <BrowserFailure
            failure={failure}
            onRetry={() => open(failure.url)}
            onExternal={() => host().openExternal(failure.url)}
            onTrust={() => void host().trustBrowserCertificate(target)}
          />
        ) : (
          frame && <img className="bframe" src={frame} alt="" draggable={false} />
        )}
      </div>
    </div>
  );
}

export function BrowserPanel(props: ManualProps | { tabs: BrowserTab[]; shown: boolean }) {
  return "tabs" in props ? <AgentBrowserPanel {...props} /> : <ManualBrowserPanel {...props} />;
}
