"use strict";

// The browser the kernel drives when it runs inside this window. The kernel
// speaks the debugging protocol to what it takes for a browser; this answers
// the browser-level half itself — targets are views this process owns — and
// hands each page's commands to that page's own debugger. Nothing here decides
// what a page may do: the kernel's policy runs before a command is sent, and
// the views' guards run in the process that draws them.

const PAGE_SESSION = "page:";

class BrowserProtocol {
  // createView({ partition, url, onEvent, onClosed, onPopup, onDownload })
  // returns { targetId, send(method, params), close(), mainFrame() }.
  // post(frame) delivers one frame back to the kernel.
  constructor({ createView, post, onActivate = () => {} }) {
    this.createView = createView;
    this.post = post;
    this.onActivate = onActivate;
    this.conns = new Map();
  }

  receive(frame) {
    if (!frame || typeof frame.conn !== "string") return;
    if (frame.open) {
      this.conns.set(frame.conn, { partition: frame.open, views: new Map() });
      return;
    }
    const conn = this.conns.get(frame.conn);
    if (!conn) return;
    if (frame.close) {
      this.closeAll(conn);
      this.conns.delete(frame.conn);
      return;
    }
    if (frame.message) this.command(frame.conn, conn, frame.message);
  }

  // drop forgets every connection the kernel's stream carried: when the stream
  // ends the kernel has already ended them, and their pages go with them.
  drop() {
    const conns = [...this.conns.values()];
    this.conns.clear();
    for (const conn of conns) this.closeAll(conn);
  }

  closeAll(conn) {
    for (const view of [...conn.views.values()]) view.close();
    conn.views.clear();
  }

  command(connId, conn, raw) {
    let msg;
    try {
      msg = typeof raw === "string" ? JSON.parse(raw) : raw;
    } catch {
      return;
    }
    const reply = (result) => this.post({ conn: connId, message: { id: msg.id, result: result ?? {} } });
    const refuse = (message, code = -32000) => this.post({ conn: connId, message: { id: msg.id, error: { code, message } } });
    if (msg.sessionId) {
      const targetId = msg.sessionId.startsWith(PAGE_SESSION) ? msg.sessionId.slice(PAGE_SESSION.length) : "";
      const view = conn.views.get(targetId);
      if (!view) return refuse(`no page for session ${msg.sessionId}`);
      if (msg.method === "Page.bringToFront") {
        this.onActivate(targetId);
        return reply({});
      }
      return Promise.resolve()
        .then(() => view.send(msg.method, msg.params || {}))
        .then(reply, (err) => refuse(String((err && err.message) || err)));
    }
    const params = msg.params || {};
    switch (msg.method) {
      case "Browser.getVersion":
        return reply({ protocolVersion: "1.3", product: "Reasonix Studio" });
      case "Target.setDiscoverTargets":
      case "Browser.setDownloadBehavior":
        return reply({});
      case "Target.createTarget": {
        const view = this.open(connId, conn, params.url || "about:blank");
        return reply({ targetId: view.targetId });
      }
      case "Target.attachToTarget":
        if (!conn.views.has(params.targetId)) return refuse(`no target ${params.targetId}`);
        return reply({ sessionId: PAGE_SESSION + params.targetId });
      case "Target.closeTarget": {
        const view = conn.views.get(params.targetId);
        if (view) view.close();
        return reply({ success: !!view });
      }
      case "Browser.close":
        this.closeAll(conn);
        return reply({});
      default:
        return refuse(`${msg.method} is not available in this window`, -32601);
    }
  }

  open(connId, conn, url) {
    const event = (method, params, sessionId) =>
      this.post({ conn: connId, message: sessionId ? { method, params, sessionId } : { method, params } });
    let targetId = "";
    const view = this.createView({
      partition: conn.partition,
      url,
      onEvent: (method, params) => event(method, params, PAGE_SESSION + targetId),
      onClosed: () => {
        if (!conn.views.delete(targetId)) return;
        event("Target.targetDestroyed", { targetId });
      },
      onPopup: (popupUrl) => {
        const popup = this.open(connId, conn, popupUrl);
        event("Target.targetCreated", { targetInfo: { targetId: popup.targetId, type: "page", openerId: targetId, url: popupUrl } });
      },
      onDownload: ({ url: from, suggestedFilename }) =>
        event("Browser.downloadWillBegin", { frameId: view.mainFrame(), url: from, suggestedFilename, guid: `${targetId}-${Date.now()}` }),
    });
    targetId = view.targetId;
    conn.views.set(targetId, view);
    return view;
  }
}

module.exports = { BrowserProtocol, PAGE_SESSION };
