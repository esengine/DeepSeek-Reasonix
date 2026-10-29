import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type FormEvent, type KeyboardEvent } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, BrowserTab } from "../port/port";
import { host, type BrowserControl, type BrowserLoadFailure, type BrowserRefusal, type PickedDetail, type PickedElement } from "../port/host";
import type { PickedSnapshot } from "./composerChips";
import { PickIcon } from "./pickicon";
import { elementBlock } from "./composerChips";
import { pressedChord } from "./keys";
import { SWAP_MARK } from "./swap";
import { occluded, visibleBox } from "./occlusion";
import { BrowserFailure } from "./BrowserFailure";
import { AgentPageNote } from "./AgentPageNote";
import { readPageTabs } from "./browser_open";

const START = "reasonix://start";

function normalise(value: string): string | null {
  const raw = value.trim();
  if (!raw) return START;
  if (/^reasonix:\/\//i.test(raw)) return START;
  // This panel frames a web page, and a frame cannot show a file: a path on
  // disk is refused here, never read as a host named by its drive letter.
  if (/^(?:[a-z]:[\\/]|\\\\|\/)/i.test(raw)) return null;
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
    let live = true;
    readPageTabs(port).then((next) => live && setTabs(next), () => {});
    return () => { live = false; };
  }, [port, moved]);
  return tabs;
}

export function localFilePath(url: string): string {
  let parsed: URL;
  try {
    parsed = new URL(url);
  } catch {
    return "";
  }
  if (parsed.protocol !== "file:" || parsed.host) return "";
  try {
    const path = decodeURIComponent(parsed.pathname);
    return /^\/[A-Za-z]:\//.test(path) ? path.slice(1) : path;
  } catch {
    return "";
  }
}

type OpenDocument = (path: string) => Promise<void>;

type Picked = NonNullable<PickedElement["element"]>;

// What the chat is handed when the person picks an element: the page it came
// from, how to find it again, and enough of it to see what they pointed at.
// The selector is the part that makes the element actionable, so a model can
// reach it again with browser_act's eval step.
// The page's fields under the names a chip carries: the reference text keeps
// the long names, the chip is data and does not need them.
function snapshotOf(element: PickedDetail): PickedSnapshot {
  return {
    url: element.pageUrl,
    title: element.pageTitle,
    tag: element.tagName,
    selector: element.selector,
    ...(element.selectorBasis ? { selectorBasis: element.selectorBasis } : {}),
    xpath: element.xpath,
    ...(element.role ? { role: element.role } : {}),
    ...(element.accessibleName ? { name: element.accessibleName } : {}),
    ...(element.text ? { text: element.text } : {}),
    ...(element.nearbyText ? { nearby: element.nearbyText } : {}),
    html: element.htmlExcerpt,
    attributes: element.attributes,
    rect: element.rect,
    style: element.style,
  };
}

// A data URL from the shell, as a file the composer's own attachment path can
// take: the bytes are already in hand, so no path round-trip is needed.
function dataUrlFile(dataUrl: string, name: string): File {
  const base64 = dataUrl.slice(dataUrl.indexOf(",") + 1);
  const bytes = Uint8Array.from(atob(base64), (ch) => ch.charCodeAt(0));
  return new File([bytes], name, { type: "image/png" });
}

export function elementReference(element: Picked, workspaceRoot?: string): string {
  return elementBlock(snapshotOf(element), workspaceRoot);
}

// What the panel says when a pick ends without an element. "Nothing happened"
// is not an answer: the ends a person can meet are different, and only the
// ones this window caused need saying aloud.
export function pickNoticeFor(reason: PickedElement["reason"]): string {
  if (reason === "timeout") return t("拾取超时，未选中元素");
  if (reason === "hidden") return t("页面被遮挡，拾取已取消");
  if (reason === "navigation" || reason === "gone") return t("页面已变化，拾取已取消");
  return "";
}

export function AgentBrowserPanel(props: { tabs: BrowserTab[]; shown: boolean; showTabs?: boolean; onOpenDocument?: OpenDocument; onAddToChat?: (reference: string) => void; onAddElement?: (picked: PickedSnapshot) => void; onAddElements?: (picked: PickedSnapshot[]) => void; onAddFiles?: (files: File[]) => void; workspaceRoot?: string }) {
  if (host().drawsBrowserViews()) return <AgentBrowserView {...props} />;
  const current = props.tabs.find((tab) => tab.active) ?? props.tabs[0];
  return <div className="bpanel">{current && <AgentPageNote tab={current} />}</div>;
}

function AgentBrowserView({ tabs, shown, showTabs = true, onOpenDocument, onAddToChat, onAddElement, onAddElements, onAddFiles, workspaceRoot }: { tabs: BrowserTab[]; shown: boolean; showTabs?: boolean; onOpenDocument?: OpenDocument; onAddToChat?: (reference: string) => void; onAddElement?: (picked: PickedSnapshot) => void; onAddElements?: (picked: PickedSnapshot[]) => void; onAddFiles?: (files: File[]) => void; workspaceRoot?: string }) {
  const slot = useRef<HTMLDivElement>(null);
  const [picked, setPicked] = useState("");
  const [picking, setPicking] = useState(false);
  // What the last pick left behind: the line the panel shows, and the selector
  // its copy button hands over.
  const [pickedNote, setPickedNote] = useState<{ line: string; selector: string } | null>(null);
  const [verifyNote, setVerifyNote] = useState("");
  // Why the last pick ended without an element, when this window caused it.
  const [pickEnded, setPickEnded] = useState("");
  const current = tabs.find((tab) => tab.target === picked) ?? tabs.find((tab) => tab.active) ?? tabs[0];
  const [address, setAddress] = useState(current?.url ?? "");
  const [failures, setFailures] = useState<Record<string, BrowserLoadFailure>>({});
  const [refused, setRefused] = useState<BrowserRefusal>("");
  const [docFailed, setDocFailed] = useState("");
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
    let timer = 0;
    let live = true;
    // Which freeze is current: a picture answering an older one is stale.
    let frozen = 0;
    let freezes = 0;
    const place = () => {
      timer = 0;
      const box = visibleBox(el);
      if (!box) {
        frozen = 0;
        shell.hideBrowserView();
        return;
      }
      if (document.documentElement.hasAttribute(SWAP_MARK) || occluded(el, box)) {
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
    const schedule = () => { if (!timer) timer = window.setTimeout(place, 48); };
    place();
    const resize = new ResizeObserver(schedule);
    resize.observe(el);
    const overlays = new MutationObserver(schedule);
    overlays.observe(document.body, { childList: true, subtree: true, attributes: true });
    overlays.observe(document.documentElement, { attributes: true, attributeFilter: [SWAP_MARK, "style", "class"] });
    // A layer can leave without touching the DOM: a transition or an animation
    // ending, a hover, a scroll. The two events catch most of it; the poll is what
    // makes a page that is no longer covered come back whatever moved.
    document.addEventListener("transitionend", schedule, true);
    document.addEventListener("animationend", schedule, true);
    addEventListener("resize", schedule);
    const poll = window.setInterval(schedule, 400);
    return () => {
      live = false;
      if (timer) clearTimeout(timer);
      clearInterval(poll);
      resize.disconnect();
      overlays.disconnect();
      document.removeEventListener("transitionend", schedule, true);
      document.removeEventListener("animationend", schedule, true);
      removeEventListener("resize", schedule);
      shell.hideBrowserView();
    };
  }, [shown, target, failure]);

  const control = (action: BrowserControl) => target && host().controlBrowserView(target, action);
  const open = (to: string) => {
    if (!target) return;
    void host()
      .navigateBrowserView(target, to)
      .then(setRefused);
  };
  const documentPath = onOpenDocument ? localFilePath(current?.url ?? "") : "";
  useEffect(() => setDocFailed(""), [current?.url]);
  const openDocument = () => {
    if (!documentPath || !onOpenDocument) return;
    setDocFailed("");
    onOpenDocument(documentPath).catch((e) => setDocFailed(reason(e)));
  };
  const go = (event: FormEvent) => {
    event.preventDefault();
    open(address);
  };
  // A pick is the person's: it answers when they choose or give up, and a
  // newer pick, a tab change or a closed panel settles the older one.
  const pickTurn = useRef(0);
  const pickingRef = useRef(false);
  useEffect(() => {
    pickingRef.current = picking;
  }, [picking]);
  const stopPick = useCallback(() => {
    pickTurn.current++;
    setPicking(false);
    if (target) host().cancelBrowserPick(target);
  }, [target]);
  const startPick = useCallback(async () => {
    if (!target) return;
    const mine = ++pickTurn.current;
    setPicking(true);
    setPickedNote(null);
    setPickEnded("");
    const labels = {
      color: t("颜色"),
      background: t("背景"),
      font: t("字体"),
      frame: t("元素在嵌入框架内，暂不支持拾取"),
      shadow: t("元素在 Shadow DOM 内，暂不支持拾取"),
    };
    const answer = await host().pickBrowserElement(target, labels).catch(() => ({ cancelled: true, reason: "gone" }) as PickedElement);
    if (mine !== pickTurn.current) return;
    setPicking(false);
    const picked = answer.elements?.length ? answer.elements : answer.element ? [answer.element] : [];
    if (answer.cancelled || picked.length === 0) {
      setPickEnded(pickNoticeFor(answer.reason));
      return;
    }
    const first = picked[0];
    setPickedNote({
      line: `${first.tagName} ${Math.round(first.rect.width)}×${Math.round(first.rect.height)}${picked.length > 1 ? ` +${picked.length - 1}` : ""}`,
      selector: first.selector,
    });
    // The chat gets the whole element as a chip; the clipboard gets the one
    // field worth pasting somewhere else.
    if (picked.length > 1 && onAddElements) onAddElements(picked.map(snapshotOf));
    else if (onAddElement) onAddElement(snapshotOf(first));
    else onAddToChat?.(elementReference(first, workspaceRoot));
    // The picture is what the person saw, so it rides along as an attachment.
    if (answer.shot && onAddFiles) onAddFiles([dataUrlFile(answer.shot, `${first.tagName.toLowerCase()}.png`)]);
    void navigator.clipboard?.writeText(first.selector).catch(() => {});
  }, [target, onAddToChat, onAddElement, onAddElements, onAddFiles, workspaceRoot]);
  // The page may have changed since the pick: re-finding the selector is how a
  // person checks that an edit landed where they pointed.
  const verify = useCallback(async () => {
    if (!target || !pickedNote) return;
    const answer = await host().verifyBrowserElement(target, pickedNote.selector).catch(() => ({ found: false, shot: "" }));
    setVerifyNote(answer.found ? t("元素仍在页面上") : t("页面上找不到这个元素了"));
    if (answer.shot && onAddFiles) onAddFiles([dataUrlFile(answer.shot, "element-again.png")]);
  }, [target, pickedNote, onAddFiles]);
  useEffect(() => {
    const onKey = (event: globalThis.KeyboardEvent) => {
      if (event.key === "Escape" && pickingRef.current) {
        event.preventDefault();
        stopPick();
        return;
      }
      if (!pressedChord(event, "c", true)) return;
      event.preventDefault();
      if (pickingRef.current) stopPick();
      else void startPick();
    };
    addEventListener("keydown", onKey);
    return () => removeEventListener("keydown", onKey);
  }, [stopPick, startPick]);
  // The page holds the keyboard while the person is looking at it, so the key
  // arrives from the shell instead of from this page.
  useEffect(
    () =>
      host().onBrowserPickShortcut((id) => {
        if (id !== target) return;
        if (pickingRef.current) stopPick();
        else void startPick();
      }),
    [target, stopPick, startPick],
  );
  // A tab that goes away, or a panel closed under a waiting pick, leaves the
  // page armed no longer.
  useEffect(
    () => () => {
      if (!pickingRef.current) return;
      pickTurn.current++;
      host().cancelBrowserPick(target);
    },
    [target],
  );
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
          <button className="bbtn" data-action="browser.pick" data-target={current?.id} aria-label={picking ? t("取消选择元素") : t("选择网页元素")} aria-pressed={picking} title={picking ? t("取消选择元素") : t("选择网页元素")} onClick={() => (picking ? stopPick() : void startPick())}><PickIcon /></button>
          <button className="bbtn" data-action="browser.external" disabled={!documentPath && !/^https?:\/\//i.test(current?.url ?? "")} aria-label={documentPath ? t("用系统默认程序打开这个文件") : t("用默认浏览器打开此页")} title={documentPath ? t("用系统默认程序打开这个文件") : t("用默认浏览器打开此页")} onClick={() => (documentPath ? openDocument() : current && void host().openExternal(current.url))}>↗</button>
          <form className="baddr" data-action="browser.navigate" data-target={current?.id} onSubmit={go}>
            <input data-action="browser.navigate" data-target={current?.id} value={address} spellCheck={false} aria-label={t("网址")} onChange={(event) => { setAddress(event.target.value); setRefused(""); }} />
          </form>
        </div>
      </div>
      {pickEnded && <p className="studio-browser-note" data-action="browser.pick-ended"><span>{pickEnded}</span></p>}
      {pickedNote && (
        <p className="studio-browser-note" data-action="browser.picked">
          <span title={pickedNote.selector}>{t("已拾取元素，选择器已复制")} · {pickedNote.line}</span>
          <button type="button" onClick={() => void navigator.clipboard?.writeText(pickedNote.selector).catch(() => {})}>{t("复制选择器")}</button>
          <button type="button" data-action="browser.verify" onClick={() => void verify()}>{t("重新定位")}</button>
          {verifyNote && <span data-action="browser.verify-result">{verifyNote}</span>}
        </p>
      )}
      {documentPath && <p className="studio-browser-note" data-action="browser.document-hint">{t("用系统默认程序打开这个文件，网页里的脚本会照常运行")}</p>}
      {docFailed && <p className="studio-browser-error" role="alert">{docFailed}</p>}
      {refused && (
        <p className="studio-browser-error" role="alert">
          {refused === "network_file" ? t("不能直接打开网络共享路径。请先把文件放到本机，再输入它的本机路径。") : t("请输入有效的 http 或 https 地址")}
        </p>
      )}
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

export function BrowserPanel(props: ManualProps | { tabs: BrowserTab[]; shown: boolean; onOpenDocument?: OpenDocument; onAddToChat?: (reference: string) => void; onAddElement?: (picked: PickedSnapshot) => void; onAddElements?: (picked: PickedSnapshot[]) => void; onAddFiles?: (files: File[]) => void; workspaceRoot?: string }) {
  return "tabs" in props ? <AgentBrowserPanel {...props} /> : <ManualBrowserPanel {...props} />;
}
