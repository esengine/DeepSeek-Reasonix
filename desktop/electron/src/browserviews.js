"use strict";
const crypto = require("node:crypto");
const { WebContentsView, session } = require("electron");
const { installPaneReload } = require("./reload");
const { guestNavigationAllowed, typedAddress } = require("./browserguard");

// The size a page lays out at while nobody is looking at it: the kernel drives
// pages whether or not the panel is open, and a page sized to nothing reads as
// a phone layout or not at all.
const AGENT_VIEWPORT = { width: 1280, height: 900 };

// CSS cannot clip a native view, so the page's corners are rounded here; the
// value is .bview's border-radius in studio.css, whose rect the view fills.
const PAGE_RADIUS = 8;

// BrowserViews owns the pages the agent's browser opens in this window. Each is
// a view on its own partition — never the session the window's credential
// lives in. A page nobody is watching is not hidden and not moved wholly off
// screen: Chromium lays out nothing outside the window, and a page it created
// while its view was hidden takes no input until shown. It is kept overlapping
// the window by one pixel in the bottom-left corner instead.
class BrowserViews {
  constructor({ win, kernelOrigin }) {
    this.win = win;
    this.kernelOrigin = kernelOrigin;
    this.entries = new Map();
    this.guarded = new Set();
    this.logins = new Map();
    // host + certificate fingerprint pairs the person chose to proceed past,
    // for this run only: a restart, or a different certificate, asks again.
    this.trusted = new Set();
    this.shown = "";
    // Moves with every show and hide, so a freeze that finishes after one does
    // not put away a page shown since.
    this.placed = 0;
    win.on("resize", () => {
      for (const [id, entry] of this.entries) if (id !== this.shown) this.putAway(entry.view);
    });
    // A page in a minimized window keeps its widget hidden, and a hidden widget
    // drops the input the agent sends it — the protocol call does not even
    // answer. Attaching the view again is what brings the widget back.
    for (const state of ["minimize", "restore", "hide", "show"]) win.on(state, () => this.reattach());
  }

  // guard fixes what every page on a partition may do: ask for no permission,
  // download nothing, and reach nothing on the kernel's origin, from any frame.
  guard(partition) {
    if (this.guarded.has(partition)) return;
    this.guarded.add(partition);
    const ses = session.fromPartition("persist:" + partition);
    ses.setPermissionRequestHandler((_wc, _permission, done) => done(false));
    ses.setPermissionCheckHandler(() => false);
    ses.webRequest.onBeforeRequest((details, done) => {
      let cancel = false;
      try {
        cancel = new URL(details.url).origin === this.kernelOrigin;
      } catch {
        cancel = false;
      }
      done({ cancel });
    });
    ses.on("will-download", (event, item, contents) => {
      event.preventDefault();
      for (const entry of this.entries.values()) {
        if (entry.view.webContents === contents) {
          entry.onDownload({ url: item.getURL(), suggestedFilename: item.getFilename() });
        }
      }
    });
  }

  create({ partition, url, onEvent, onClosed, onPopup, onDownload }) {
    this.guard(partition);
    const targetId = crypto.randomUUID();
    const view = new WebContentsView({
      webPreferences: {
        partition: "persist:" + partition,
        sandbox: true,
        contextIsolation: true,
        nodeIntegration: false,
        webviewTag: false,
        webSecurity: true,
        backgroundThrottling: false,
      },
    });
    view.setBorderRadius(PAGE_RADIUS);
    const contents = view.webContents;
    let mainFrame = "";
    const allowed = (to) => guestNavigationAllowed(to, this.kernelOrigin);
    contents.on("will-navigate", (event, to) => {
      if (!allowed(to)) event.preventDefault();
    });
    contents.on("will-redirect", (event, to) => {
      if (!allowed(to)) event.preventDefault();
    });
    contents.setWindowOpenHandler(({ url: to }) => {
      if (allowed(to)) onPopup(to);
      return { action: "deny" };
    });
    installPaneReload(contents);
    contents.debugger.attach("1.3");
    contents.debugger.on("message", (_event, method, params, sessionId) => {
      if (sessionId) return;
      if (method === "Page.frameNavigated" && params && params.frame && !params.frame.parentId) {
        mainFrame = params.frame.id;
      }
      onEvent(method, params);
    });
    const entry = { view, onDownload, typed: 0 };
    this.watchLoads(targetId, entry);
    let closed = false;
    const close = () => {
      if (closed) return;
      closed = true;
      this.entries.delete(targetId);
      if (!this.win.isDestroyed()) this.win.contentView.removeChildView(view);
      if (!contents.isDestroyed()) contents.close();
      onClosed();
    };
    contents.once("destroyed", close);
    contents.on("render-process-gone", close);
    this.entries.set(targetId, entry);
    this.win.contentView.addChildView(view);
    this.putAway(view);
    void contents.loadURL(url).catch(() => {});
    return {
      targetId,
      send: (method, params) => contents.debugger.sendCommand(method, params),
      close,
      mainFrame: () => mainFrame,
    };
  }

  // show draws one page over the rectangle the page reserved for it, in window
  // coordinates, and puts every other page away.
  show(targetId, rect) {
    const entry = this.entries.get(targetId);
    if (!entry || !validRect(rect)) return this.hide();
    const at = {
      x: Math.round(rect.x),
      y: Math.round(rect.y),
      width: Math.round(rect.width),
      height: Math.round(rect.height),
    };
    // The panel reports its rectangle whenever anything in the window changes,
    // which during a turn is constantly, and nearly always the same rectangle:
    // re-adding the view and setting the bounds it already has is work for
    // every one of those.
    if (this.shown === targetId && sameRect(entry.at, at)) return;
    this.placed++;
    for (const [id, other] of this.entries) {
      if (id !== targetId) this.putAway(other.view, other);
    }
    this.shown = targetId;
    this.win.contentView.addChildView(entry.view);
    entry.view.setBounds(at);
    entry.at = at;
  }

  reattach() {
    for (const [id, entry] of this.entries) {
      if (this.win.isDestroyed() || entry.view.webContents.isDestroyed()) continue;
      this.win.contentView.removeChildView(entry.view);
      this.win.contentView.addChildView(entry.view);
      if (id !== this.shown) this.putAway(entry.view, entry);
    }
  }

  // freeze answers with a picture of the page on screen, taken before it is put
  // away, for the window to draw in its place while something of its own is
  // over it. Only the page already shown to the person is ever taken, and the
  // picture goes to this window's page and nowhere else.
  async freeze() {
    const id = this.shown;
    const entry = this.entries.get(id);
    if (!entry || entry.view.webContents.isDestroyed()) {
      this.hide();
      return "";
    }
    const turn = this.placed;
    let picture = "";
    try {
      const image = await entry.view.webContents.capturePage();
      if (!image.isEmpty()) picture = "data:image/jpeg;base64," + image.toJPEG(85).toString("base64");
    } catch {
      picture = "";
    }
    if (this.placed === turn && this.shown === id) this.hide();
    return picture;
  }

  hide() {
    this.placed++;
    this.shown = "";
    for (const entry of this.entries.values()) this.putAway(entry.view, entry);
  }

  putAway(view, entry) {
    const { height } = this.win.getContentBounds();
    view.setBounds({ x: 1 - AGENT_VIEWPORT.width, y: height - 1, ...AGENT_VIEWPORT });
    if (entry) entry.at = null;
  }

  control(targetId, action) {
    const contents = this.entries.get(targetId)?.view.webContents;
    if (!contents) return;
    const history = contents.navigationHistory;
    switch (action) {
      case "back":
        if (history.canGoBack()) history.goBack();
        break;
      case "forward":
        if (history.canGoForward()) history.goForward();
        break;
      case "reload":
        contents.reload();
        break;
      case "stop":
        contents.stop();
        break;
    }
  }

  navigate(targetId, address) {
    const entry = this.entries.get(targetId);
    const { url, fallback } = typedAddress(address);
    if (!entry || !guestNavigationAllowed(url, this.kernelOrigin)) return false;
    void this.loadTyped(targetId, entry, url, fallback);
    return true;
  }

  // loadTyped owns the outcome of an address the person typed. loadURL's own
  // answer is read rather than did-fail-load: a refused port or scheme never
  // starts a navigation, so no event would say why nothing happened.
  async loadTyped(targetId, entry, url, fallback) {
    const turn = ++entry.typed;
    const contents = entry.view.webContents;
    let failed = await loadResult(contents, url);
    if (failed && fallback && entry.typed === turn) failed = await loadResult(contents, fallback);
    if (entry.typed !== turn) return;
    entry.typed = 0;
    if (failed) this.report(targetId, failed, entry);
  }

  // watchLoads tells the window why a page did not load, since Electron draws
  // no error page of its own, and answers a login a page or a proxy asks for
  // with what the person types rather than cancelling it.
  watchLoads(targetId, entry) {
    const contents = entry.view.webContents;
    contents.on("did-start-navigation", (details) => {
      if (details.isMainFrame && !details.isSameDocument) this.report(targetId, null);
    });
    contents.on("did-fail-load", (_event, code, description, url, isMainFrame) => {
      // -3 is ERR_ABORTED: a navigation replaced by another, or stopped. A
      // typed address reports through loadTyped, which may still retry it.
      if (!isMainFrame || code === -3 || entry.typed) return;
      this.report(targetId, { url, code, reason: description }, entry);
    });
    // Electron rejects a certificate it cannot verify unless told otherwise,
    // and only a pair the person trusted through trustCertificate is.
    contents.on("certificate-error", (event, url, error, certificate, callback, isMainFrame) => {
      const host = hostOf(url);
      if (this.trusted.has(trustKey(host, certificate.fingerprint))) {
        event.preventDefault();
        callback(true);
        return;
      }
      if (isMainFrame) {
        entry.certificate = {
          host,
          url,
          error,
          fingerprint: certificate.fingerprint,
          subject: certificate.subjectName,
          issuer: certificate.issuerName,
          expiry: certificate.validExpiry,
        };
      }
    });
    contents.on("login", (event, details, authInfo, callback) => {
      event.preventDefault();
      const id = crypto.randomUUID();
      this.logins.set(id, callback);
      this.send("browser:login", {
        id,
        targetId,
        url: details.url,
        host: authInfo.host,
        port: authInfo.port,
        realm: authInfo.realm,
        proxy: authInfo.isProxy,
      });
    });
  }

  // answerLogin settles one login the window was asked for. No answer, or an
  // empty one, cancels it the way Electron would have.
  answerLogin(id, username, password) {
    const callback = this.logins.get(id);
    if (!callback) return;
    this.logins.delete(id);
    if (username) callback(username, password);
    else callback();
  }

  report(targetId, failure, entry) {
    const cert = entry?.certificate;
    if (failure && cert && failure.reason.startsWith("ERR_CERT_") && cert.host === hostOf(failure.url)) {
      const { host, error, fingerprint, subject, issuer, expiry } = cert;
      failure = { ...failure, certificate: { host, error, fingerprint, subject, issuer, expiry } };
    }
    this.send("browser:load-state", { targetId, failure });
  }

  // trustCertificate proceeds past the certificate a page's last load was
  // refused for. It is reached only from the window's own page, so neither
  // the kernel nor the agent driving the browser can trust one.
  trustCertificate(targetId) {
    const entry = this.entries.get(targetId);
    const cert = entry?.certificate;
    if (!cert) return false;
    this.trusted.add(trustKey(cert.host, cert.fingerprint));
    entry.certificate = null;
    void this.loadTyped(targetId, entry, cert.url, "");
    return true;
  }

  send(channel, payload) {
    if (!this.win.isDestroyed()) this.win.webContents.send(channel, payload);
  }

  closeAll() {
    for (const id of [...this.logins.keys()]) this.answerLogin(id);
    for (const entry of [...this.entries.values()]) entry.view.webContents.close();
  }
}

function hostOf(raw) {
  try {
    return new URL(raw).host;
  } catch {
    return "";
  }
}

function trustKey(host, fingerprint) {
  return `${host} ${fingerprint}`;
}

// loadResult is null when the page loaded, or why it did not. An abort is
// another navigation taking over, which is not a failure of this one.
async function loadResult(contents, url) {
  try {
    await contents.loadURL(url);
    return null;
  } catch (err) {
    if (err?.code === "ERR_ABORTED") return null;
    return { url: err?.url || url, code: Number(err?.errno) || 0, reason: String(err?.code || "") };
  }
}

function sameRect(a, b) {
  return !!a && a.x === b.x && a.y === b.y && a.width === b.width && a.height === b.height;
}

function validRect(rect) {
  return rect && [rect.x, rect.y, rect.width, rect.height].every(Number.isFinite) && rect.width > 0 && rect.height > 0;
}

module.exports = { BrowserViews, AGENT_VIEWPORT };
