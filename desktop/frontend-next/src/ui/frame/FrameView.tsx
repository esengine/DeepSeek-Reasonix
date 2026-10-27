import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { t } from "../../i18n";
import { say } from "../../i18n/kernel";
import type { ExtensionSurface } from "../../port/wire";
import type { Slot } from "../slots";
import { clampHeight, decodeFrameMessage, DropCounter, FRAME_PROTOCOL, RateGate, SLOT_HEIGHTS, themeTokens } from "./bridge";
import { FrameState, frameKey } from "./state";

/** FrameHost is what a frame may reach through Studio: its own document's
 *  address, and its own plugin's declared actions and submit. */
export interface FrameHost {
  state: FrameState;
  url(ext: ExtensionSurface): string;
  act(ext: ExtensionSurface, actionId: string, args?: Record<string, string>): void;
  submit(ext: ExtensionSurface, values: Record<string, unknown>): void;
}

const theme = () => ({ scheme: document.documentElement.dataset.theme ?? "", tokens: themeTokens() });

type Phase = { kind: "checking" } | { kind: "ready" } | { kind: "failed"; why: string };

export function FrameView({ ext, slot, host }: { ext: ExtensionSurface; slot: Slot; host: FrameHost }) {
  const identity = JSON.stringify([host.state.scope, host.url(ext), frameKey(ext)]);
  return <FrameDocument key={identity} ext={ext} slot={slot} host={host} />;
}

function FrameDocument({ ext, slot, host }: { ext: ExtensionSurface; slot: Slot; host: FrameHost }) {
  const doc = ext.frame!;
  const key = frameKey(ext);
  const url = host.url(ext);
  const [phase, setPhase] = useState<Phase>({ kind: "checking" });
  const [attempt, setAttempt] = useState(0);
  const [height, setHeight] = useState<number | undefined>(clampHeight(0, SLOT_HEIGHTS[slot]));
  const channel = useRef<{ port: MessagePort; ready: boolean } | null>(null);
  const live = useRef({ ext, host, slot });
  live.current = { ext, host, slot };

  useEffect(() => {
    let current = true;
    const abort = new AbortController();
    setPhase({ kind: "checking" });
    fetch(url, { credentials: "same-origin", cache: "no-store", signal: abort.signal })
      .then(async (res) => {
        if (res.ok) return current && setPhase({ kind: "ready" });
        const body = (await res.json().catch(() => null)) as { code?: string; error?: string } | null;
        if (current) setPhase({ kind: "failed", why: say(body, t("界面文档读取失败，请重试")) });
      })
      .catch(() => current && setPhase({ kind: "failed", why: t("界面文档读取失败，请重试") }));
    return () => {
      current = false;
      abort.abort();
    };
  }, [url, attempt]);

  const disconnect = () => {
    if (channel.current) {
      channel.current.port.onmessage = null;
      channel.current.port.close();
      channel.current = null;
    }
  };
  useLayoutEffect(() => disconnect, []);

  useEffect(() => {
    if (channel.current?.ready) channel.current.port.postMessage({ type: "props", props: doc.props ?? {} });
  }, [doc.props]);

  useEffect(() => {
    const observer = new MutationObserver(() => {
      if (channel.current?.ready) channel.current.port.postMessage({ type: "theme", theme: theme() });
    });
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme", "style", "class"] });
    return () => observer.disconnect();
  }, []);

  const close = () => {
    disconnect();
    setPhase({ kind: "failed", why: "" });
  };

  const connect = (iframe: HTMLIFrameElement) => {
    disconnect();
    const { port1, port2 } = new MessageChannel();
    const connection = { port: port1, ready: false };
    channel.current = connection;
    const gate = new RateGate();
    const drops = new DropCounter();
    port1.onmessage = (event) => {
      if (channel.current !== connection) return;
      const now = Date.now();
      const msg = decodeFrameMessage(event.data);
      if (!msg || !gate.allow(msg.type, now) || (msg.type === "ready" ? connection.ready : !connection.ready)) {
        if (drops.record(now)) close();
        return;
      }
      const { ext: current, host: reach, slot: place } = live.current;
      switch (msg.type) {
        case "ready":
          connection.ready = true;
          port1.postMessage({
            type: "init", protocol: FRAME_PROTOCOL, props: current.frame?.props ?? {}, theme: theme(),
            locale: document.documentElement.lang || navigator.language, readOnly: !!current.code,
            ...host.state.opened(key),
          });
          return;
        case "size":
          setHeight(clampHeight(msg.height, SLOT_HEIGHTS[place]));
          return;
        case "action":
          if (!current.code) reach.act(current, msg.actionId, msg.args);
          return;
        case "submit":
          if (!current.code) reach.submit(current, msg.values);
          return;
        case "state":
          host.state.save(key, msg.value);
          return;
      }
    };
    iframe.contentWindow?.postMessage({ type: "connect", protocol: FRAME_PROTOCOL }, "*", [port2]);
  };

  return (
    <div className="xframe" data-slot={slot} data-phase={phase.kind}>
      <div className="xframe-hd">
        <span className="nm">{doc.title}</span>
        <span className="xframe-src">{t("扩展内容")} · {ext.pluginId}</span>
      </div>
      {phase.kind === "failed" ? (
        <div className="xframe-fb" role="status">
          <p>{doc.fallback}</p>
          {phase.why && <p className="why">{phase.why}</p>}
          <button type="button" data-action="extensions.frame-retry" onClick={() => setAttempt((n) => n + 1)}>
            {t("重试")}
          </button>
        </div>
      ) : (
        phase.kind === "ready" && (
          <iframe
            key={attempt}
            className="xframe-doc"
            src={url}
            sandbox="allow-scripts"
            allow=""
            referrerPolicy="no-referrer"
            title={`${doc.title} — ${ext.pluginId}`}
            style={height === undefined ? undefined : { height }}
            onLoad={(e) => connect(e.currentTarget)}
          />
        )
      )}
    </div>
  );
}
