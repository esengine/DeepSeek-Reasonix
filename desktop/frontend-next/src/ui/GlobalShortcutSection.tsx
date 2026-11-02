import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import { t } from "../i18n";
import { host, type GlobalShortcutInfo } from "../port/host";
import { Group } from "./Group";
import { acceleratorLabel } from "./keys";

const STATUS = {
  disabled: "全局快捷键已关闭",
  registered: "已注册：{value}",
  invalid: "快捷键格式无效，请使用修饰键与按键组合，例如 CommandOrControl+Shift+Space。",
  unavailable: "系统未能注册此快捷键：可能已被占用、未获授权，或当前桌面不支持。",
  unsupported: "当前环境不支持全局快捷键。",
};
const ERRORS = {
  invalid: STATUS.invalid,
  unavailable: STATUS.unavailable,
  save: "快捷键未能保存；原设置保持不变。",
  quitting: "Studio 正在退出，未更改快捷键。",
};

function capturedBinding(event: KeyboardEvent<HTMLInputElement>): string {
  if (event.nativeEvent.isComposing || event.repeat || event.getModifierState("AltGraph")) return "";
  const modifiers = [
    event.ctrlKey && "Control", event.altKey && "Alt", event.shiftKey && "Shift", event.metaKey && "Super",
  ].filter(Boolean);
  if (!event.ctrlKey && !event.altKey && !event.metaKey) return "";
  const names: Record<string, string> = { " ": "Space", "+": "Plus", ArrowUp: "Up", ArrowDown: "Down", ArrowLeft: "Left", ArrowRight: "Right" };
  const punctuation: Record<string, string> = { Comma: ",", Period: ".", Slash: "/", Semicolon: ";", Quote: "'", BracketLeft: "[", BracketRight: "]", Backslash: "\\", Backquote: "`", Minus: "-", Equal: "=" };
  const position = /^(Key[A-Z]|Digit[0-9])$/.test(event.code) ? event.code.replace(/^(Key|Digit)/, "") : punctuation[event.code];
  const key = names[event.key] ?? (/^[a-z0-9]$/i.test(event.key) ? event.key.toUpperCase() : position ?? event.key);
  if (!/^(Space|Plus|Up|Down|Left|Right|Enter|Escape|Backspace|Delete|Insert|Home|End|PageUp|PageDown|F([1-9]|1[0-9]|2[0-4])|[A-Z0-9,./;'\[\]\\`=-])$/.test(key)) return "";
  return [...modifiers, key].join("+");
}

export function GlobalShortcutSection() {
  const [now, setNow] = useState<GlobalShortcutInfo | null>(null);
  const [draft, setDraft] = useState("");
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  const [recording, setRecording] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => {
    let live = true;
    host().globalShortcut().then(value => {
      if (live) { setNow(value); setDraft(value.binding); }
    }).catch(() => live && setFailed(true));
    return () => { live = false; };
  }, []);
  if (!host().inShell() || now?.status === "unsupported") return null;
  async function apply(binding: string) {
    setBusy(true);
    setFailed(false);
    try {
      const value = await host().setGlobalShortcut(binding);
      setNow(value);
      if (!value.error) setDraft(value.binding);
    } catch {
      setFailed(true);
    } finally {
      setBusy(false);
    }
  }
  return (
    <Group id="global-shortcut" title={t("全局显示 / 隐藏快捷键")} hint={t("默认关闭。仅在 Studio 运行时生效；窗口聚焦时隐藏，否则显示并聚焦。不会启动已退出的应用，也不更改窗口内快捷键。") }>
      <form data-action="window.global-shortcut" data-value="apply" onSubmit={event => { event.preventDefault(); void apply(draft); }}>
        <label className="lrow">
          <span className="tx"><span className="lb">{t("快捷键组合")}</span></span>
          <input ref={input} className="fontown" data-action-change="window.global-shortcut" data-action-keydown="window.global-shortcut" data-value="binding" aria-label={t("快捷键组合")} maxLength={128} placeholder={recording ? t("请按修饰键与按键组合…") : "CommandOrControl+Shift+Space"} value={draft} readOnly={recording} onChange={event => setDraft(event.target.value)} onBlur={() => setRecording(false)} onKeyDown={event => {
            if (!recording || event.key === "Tab") return;
            event.preventDefault(); event.stopPropagation();
            if (event.key === "Escape" && !event.ctrlKey && !event.altKey && !event.shiftKey && !event.metaKey) { setRecording(false); return; }
            const binding = capturedBinding(event);
            if (binding) { setDraft(binding); setRecording(false); }
          }} disabled={!now || busy} />
        </label>
        {draft && <p className="note"><kbd>{acceleratorLabel(draft)}</kbd></p>}
        <p className="note">{t("点击录制后按下修饰键与按键组合，或直接编辑快捷键。Esc 取消录制；清除会立即关闭快捷键。Wayland 或系统保留组合可能不受支持。")}</p>
        <div className="seg" data-text>
          <button type="button" data-action="window.global-shortcut" data-value="record" disabled={!now || busy} onClick={() => { setRecording(true); input.current?.focus(); }}>{t("录制快捷键")}</button>
          <button type="submit" data-action="window.global-shortcut" data-value="apply" disabled={!now || busy}>{t("应用快捷键")}</button>
          <button type="button" data-action="window.global-shortcut" data-value="disable" disabled={!now || busy || (!now.binding && !draft)} onClick={() => void apply("")}>{t("清除快捷键")}</button>
        </div>
      </form>
      {now && <p className="note" role="status">{t(STATUS[now.status], { value: acceleratorLabel(now.binding) })}</p>}
      {now?.error && <p className="note" role="alert">{t(ERRORS[now.error])}</p>}
      {failed && <p className="note" role="alert">{t("无法读取或更改 shell 快捷键状态。请重新打开设置后再试。")}</p>}
    </Group>
  );
}
