import { useEffect, useRef, useState } from "react";
import { AppBridge, PostMessageTransport } from "@modelcontextprotocol/ext-apps/app-bridge";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import type { MCPAppInstanceView, MCPAppPresentation } from "../lib/types";
import { normalizeMCPAppResult, parseMCPAppArguments, parseMCPAppCallResult, validatedMCPAppLinkOrigin } from "../lib/mcpAppProtocol";
import { useConfirmDialog } from "./ConfirmDialog";

const MIN_APP_HEIGHT = 120;
const MAX_APP_HEIGHT = 720;
const TEARDOWN_TIMEOUT_MS = 1000;

function clampHeight(px: number): number {
  if (!Number.isFinite(px)) return MIN_APP_HEIGHT;
  return Math.min(MAX_APP_HEIGHT, Math.max(MIN_APP_HEIGHT, Math.round(px)));
}

function nonceFromOuterURL(outerUrl: string): string {
  try {
    return new URL(outerUrl).searchParams.get("nonce") ?? "";
  } catch {
    return "";
  }
}

function teardownTimeout(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, TEARDOWN_TIMEOUT_MS));
}

// The AppBridge lifecycle is connect -> initialized -> tool input -> tool
// result -> resource teardown. Privileged callbacks retain the originating tab.
export function MCPAppCard({
  instance,
  presentation,
  toolArgs,
  toolOutput,
  onDispose,
}: {
  instance: MCPAppInstanceView;
  presentation: MCPAppPresentation;
  toolArgs: string;
  toolOutput?: string;
  onDispose?: (instanceToken: string) => void;
}) {
  const iframeRef = useRef<HTMLIFrameElement>(null);
  const linkGrantsRef = useRef(new Set<string>());
  const [height, setHeight] = useState(MIN_APP_HEIGHT);
  const { confirm, dialog } = useConfirmDialog();
  const t = useT();

  useEffect(() => {
    const frame = iframeRef.current;
    if (!frame) return;
    let disposed = false;
    let bridgeStarted = false;
    const bridge = new AppBridge(
      null,
      { name: "reasonix", version: "desktop" },
      { openLinks: {}, serverTools: {}, logging: {} },
    );

    const dispose = async (notify: boolean) => {
      if (disposed) return;
      disposed = true;
      if (bridgeStarted) {
        try {
          await Promise.race([bridge.teardownResource({}), teardownTimeout()]);
        } catch {
          // The view may already be gone; bounded host cleanup still runs.
        }
      }
      await bridge.close().catch(() => undefined);
      await app.MCPCloseAppInstanceForTab(instance.tabId, instance.instanceToken).catch(() => undefined);
      if (notify) onDispose?.(instance.instanceToken);
    };

    bridge.oncalltool = async (params) => {
      const raw = await app.MCPAppCallToolForTab(
        instance.tabId,
        instance.instanceToken,
        params.name,
        params.arguments ?? {},
      );
      return parseMCPAppCallResult(raw);
    };
    bridge.onopenlink = async (params) => {
      const origin = validatedMCPAppLinkOrigin(params.url);
      if (!origin) return { isError: true };
      if (!linkGrantsRef.current.has(origin)) {
        const allowed = await confirm({
          title: t("mcp.app.linkTitle"),
          message: t("mcp.app.linkMessage", { origin }),
          confirmLabel: t("mcp.app.linkOpen"),
          cancelLabel: t("common.cancel"),
        });
        if (!allowed) return { isError: true };
        linkGrantsRef.current.add(origin);
      }
      try {
        await app.MCPOpenAppLinkForTab(instance.tabId, instance.instanceToken, params.url);
        return {};
      } catch {
        return { isError: true };
      }
    };
    bridge.onsizechange = ({ height: nextHeight }) => {
      if (typeof nextHeight === "number") setHeight(clampHeight(nextHeight));
    };
    bridge.oninitialized = () => {
      void (async () => {
        await bridge.sendToolInput({ arguments: parseMCPAppArguments(toolArgs) });
        if (!disposed) await bridge.sendToolResult(normalizeMCPAppResult(presentation, toolOutput));
      })().catch(() => undefined);
    };
    bridge.onrequestteardown = () => { void dispose(true); };

    const nonce = nonceFromOuterURL(instance.outerUrl);
    const onFrameLoad = () => {
      const target = frame.contentWindow;
      if (!target || disposed) return;
      target.postMessage({ __mcpInit: nonce }, "*");
      bridgeStarted = true;
      void bridge.connect(new PostMessageTransport(target, target)).catch(() => { void dispose(true); });
    };
    frame.addEventListener("load", onFrameLoad);

    return () => {
      frame.removeEventListener("load", onFrameLoad);
      void dispose(false);
    };
  }, [confirm, instance, onDispose, presentation, t, toolArgs, toolOutput]);

  const src = `${instance.outerUrl}&src=${encodeURIComponent(instance.resourceQuery)}`;
  return (
    <div className="mcp-app-card" data-server={instance.server} data-resource-digest={instance.resourceDigest}>
      <iframe
        ref={iframeRef}
        className="mcp-app-frame"
        src={src}
        style={{ height: `${height}px` }}
        title={`MCP App: ${instance.server}/${instance.tool}`}
      />
      {dialog}
    </div>
  );
}
