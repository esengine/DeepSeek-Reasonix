"use strict";
const crypto = require("node:crypto");
const { WebContentsView, session } = require("electron");
const { installPaneReload } = require("./reload");
const { guestNavigationAllowed, typed } = require("./browserguard");
const { ARM, STOP } = require("./elementpick");
const { InputWake } = require("./inputwake");
const { KeyboardKeeper } = require("./keyboard");

// The size a page lays out at while nobody is looking at it: the kernel drives
// pages whether or not the panel is open, and a page sized to nothing reads as
// a phone layout or not at all.
const AGENT_VIEWPORT = { width: 1280, height: 900 };

// How long a page may be held out of the window while its navigation runs; past
// this it is shown again loaded or not, or a server that accepts the connection
// and never answers would keep the panel blank for as long as it likes.
const LEAVE_LIMIT_MS = 4000;

// CSS cannot clip a native view, so the page's corners are rounded here; the
// value is .bview's border-radius in studio.css, whose rect the view fills.
const PAGE_RADIUS = 8;

// How many pages may stay asleep at once; past this the oldest is buried.
const SLEPT_LIMIT = 18;

// How long a pick waits for the person. Past it the page is left as it was
// found: a picker left armed swallows every click on the page, so the wait is
// a minute and a half, not an afternoon.
const PICK_LIMIT_MS = 90_000;
// A page's own scripts live in world 0; the picker answers from its own.
const PICK_WORLD = 999;
// The page wrote the answer; these repeat the page script's own limits because
// the shell cannot trust it to have run them.
const TEXT_CLIP = 4000;
const NEARBY_CLIP = 300;
const MARKUP_CLIP = 6000;
const VALUE_CLIP = 500;
const ATTR_LIMIT = 64;
const STYLE_CLIP = 200;
const ELEMENT_STRINGS = ["pageUrl", "pageTitle", "tagName", "role", "accessibleName", "selector", "xpath", "text", "nearbyText", "htmlExcerpt"];
const STYLE_KEYS = ["backgroundColor", "color", "display", "fontFamily", "fontSize", "fontWeight"];
const SELECTOR_BASIS = ["testid", "aria-label", "id", "class"];
const CANCEL_REASONS = ["escape", "dismissed", "timeout", "hidden", "navigation", "gone"];
const clip = (value, limit) => (value.length > limit ? value.slice(0, limit) : value);

// A command that switches a CDP domain on. The kernel sends these once at
// attach, and a sleep throws away the session they were said on, so they are
// kept per page and said again to every session a wake builds.
const ENABLE_SHAPE = /^[A-Za-z]+\.enable$/;

// BrowserViews owns the pages the agent's browser opens in this window, each a
// view on its own partition, never the session the window's credential lives in.
// An unwatched page is parked at a one-pixel corner; an idle one sleeps whole.
class BrowserViews {
  constructor({ win, kernelOrigin, sleepAfterMs = 15 * 60_000, sweepMs = 60_000, maxAwake = 6 }) {
    this.win = win;
    this.kernelOrigin = kernelOrigin;
    this.entries = new Map();
    this.guarded = new Set();
    this.logins = new Map();
    // host + certificate fingerprint pairs the person chose to proceed past,
    // for this run only: a restart, or a different certificate, asks again.
    this.trusted = new Set();
    this.shown = "";
    this.keyboard = new KeyboardKeeper({ win });
    this.inputWake = new InputWake({ win });
    // A page whose renderer has sat untouched for sleepAfterMs is slept by the
    // sweep: a slept page keeps its entry and its targetId, so the kernel's
    // handle outlives the sleep.
    this.sleepAfterMs = sleepAfterMs;
    this.maxAwake = maxAwake;
    this.sweeper = setInterval(() => this.sweep(), sweepMs);
    // The timer holds no process open by itself: the Studio's loop is kept by
    // its windows, and a test run full of these must still be able to end.
    if (typeof this.sweeper.unref === "function") this.sweeper.unref();
    // Moves with every show and hide, so a freeze that finishes after one does
    // not put away a page shown since.
    this.placed = 0;
    win.on("resize", () => {
      for (const [id, entry] of this.entries) {
        if (id !== this.shown && entry.view) this.putAway(entry.view);
      }
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
        if (entry.view && entry.view.webContents === contents) {
          entry.onDownload({ url: item.getURL(), suggestedFilename: item.getFilename() });
        }
      }
    });
  }

  create({ partition, url, onEvent, onClosed, onPopup, onDownload }) {
    this.guard(partition);
    const targetId = crypto.randomUUID();
    // The cap on awake pages obeys the same switch as the sweep — off means
    // nothing sleeps, cap included: the least recently touched page is slept to
    // make room, never the one the person is looking at. A wake yields no room.
    let awake = 0;
    let victim = null;
    for (const [id, other] of this.entries) {
      if (other.slept) continue;
      awake++;
      if (id !== this.shown && !other.sleeping && !other.returning && (!victim || other.lastTouch < victim.lastTouch)) victim = other;
    }
    if (Number.isFinite(this.sleepAfterMs) && awake >= this.maxAwake && victim) this.sleep(victim).catch(() => {});
    const entry = {
      targetId,
      partition,
      url,
      onEvent,
      onPopup,
      onDownload,
      view: null,
      mainFrame: "",
      enables: [],
      typed: 0,
      synthUntil: 0,
      attached: false,
      panel: null,
      override: null,
      emulated: null,
      scale: 1,
      lastTouch: Date.now(),
      slept: null,
      sleeping: false,
      rising: null,
      pending: new Set(),
      showing: false,
      dialog: false,
      closed: false,
    };
    // The burial of a target is the one thing the kernel is told of, so it
    // lives on the entry rather than on the view: a page that sleeps and wakes
    // grows a new view, and it is buried by its close, not by a view's death.
    const close = () => {
      if (entry.closed) return;
      entry.closed = true;
      this.entries.delete(targetId);
      clearTimeout(entry.returning);
      if (entry.attached && !this.win.isDestroyed() && entry.view) this.win.contentView.removeChildView(entry.view);
      entry.attached = false;
      const contents = entry.view && entry.view.webContents;
      if (contents && !contents.isDestroyed()) contents.close();
      onClosed();
    };
    entry.close = close;
    this.entries.set(targetId, entry);
    try {
      this.spawn(entry);
    } catch (err) {
      // A spawn that fails half-built leaves no entry behind: the target never
      // happened, so nothing is closed and the kernel is told nothing of it.
      entry.closed = true;
      this.entries.delete(targetId);
      const contents = entry.view && entry.view.webContents;
      if (contents && !contents.isDestroyed()) contents.close();
      throw err;
    }
    return {
      targetId,
      send: (method, params) => {
        entry.lastTouch = Date.now();
        // A slept page is woken first and the call dispatched to what it comes
        // back as; a page still rising takes the call when it has landed, since
        // mid-load answers the page it was.
        const rising = entry.slept ? this.wake(entry) : entry.rising;
        if (rising) return rising.then(() => this.dispatch(entry, method, params));
        return this.dispatch(entry, method, params);
      },
      close,
      mainFrame: () => (entry.slept ? "" : entry.mainFrame),
    };
  }

  // spawn builds the view a page lives in and loads its address. It runs for
  // create() and again for every wake, so a page that slept comes back as
  // exactly what a new page is, under the handle it never lost.
  spawn(entry) {
    const view = new WebContentsView({
      webPreferences: {
        partition: "persist:" + entry.partition,
        sandbox: true,
        contextIsolation: true,
        nodeIntegration: false,
        webviewTag: false,
        webSecurity: true,
        backgroundThrottling: false,
      },
    });
    view.setBorderRadius(PAGE_RADIUS);
    entry.view = view;
    entry.mainFrame = "";
    const contents = view.webContents;
    const allowed = (to) => guestNavigationAllowed(to, this.kernelOrigin);
    contents.on("will-navigate", (event, to) => {
      if (!allowed(to)) event.preventDefault();
    });
    contents.on("will-redirect", (event, to) => {
      if (!allowed(to)) event.preventDefault();
    });
    contents.setWindowOpenHandler(({ url: to }) => {
      if (allowed(to)) entry.onPopup(to);
      return { action: "deny" };
    });
    installPaneReload(contents);
    contents.debugger.attach("1.3");
    contents.debugger.on("message", (_event, method, params, sessionId) => {
      entry.lastTouch = Date.now();
      if (sessionId) return;
      if (method === "Page.frameNavigated" && params && params.frame && !params.frame.parentId) {
        entry.mainFrame = params.frame.id;
      }
      // The kernel enables Page at attach, so the dialog events reach the
      // shell: a page waiting on its answer is not slept under the question.
      if (method === "Page.javascriptDialogOpening") entry.dialog = true;
      else if (method === "Page.javascriptDialogClosed") entry.dialog = false;
      entry.onEvent(method, params);
    });
    this.keyboard.watch(contents, () => Date.now() < entry.synthUntil);
    // The picker's key belongs to the page while the page holds the keyboard:
    // a press that went there never reaches the window's own page, so it is
    // taken here and the picker is started where the person is looking.
    contents.on("before-input-event", (event, input) => {
      if (!input || input.type !== "keyDown" || input.isAutoRepeat || !input.key || !input.shift) return;
      if (String(input.key).toLowerCase() !== "c") return;
      if (process.platform === "darwin" ? !input.meta : !input.control) return;
      if (entry.targetId !== this.shown) return;
      event.preventDefault();
      this.send("browser:pick-shortcut", { targetId: entry.targetId });
    });
    this.watchLoads(entry.targetId, entry);
    const gone = () => {
      // A sleep closes the contents itself, so a death under it is the sleep
      // landing, not the target dying: the entry stays a tombstone for its next
      // send() to raise or its close() to bury.
      if (entry.sleeping) {
        entry.sleeping = false;
        return;
      }
      if (entry.slept || entry.view?.webContents !== contents) return;
      entry.close();
    };
    contents.once("destroyed", gone);
    contents.on("render-process-gone", gone);
    // A woken page must come back laid out at the viewport the agent set, not
    // at whatever its view happens to be while it is parked.
    if (entry.emulated) contents.debugger.sendCommand("Emulation.setDeviceMetricsOverride", entry.emulated).catch(() => {});
    // The page loads before the window holds it, or the platform hands it the
    // keyboard and ends the person's composition. The load's promise is what a
    // wake waits out; a create leaves the waiting to the kernel.
    return contents.loadURL(entry.url).catch(() => {});
  }

  // dispatch carries one protocol call to the page's debugger. It runs on a
  // live view only; every caller has seen to the page being awake first.
  dispatch(entry, method, params) {
    // An enable-shape command is remembered as it is sent — latest parameters,
    // first-seen order — and a domain switched off leaves the set, so the next
    // wake does not switch it back on.
    if (ENABLE_SHAPE.test(method) || method === "Page.setLifecycleEventsEnabled") {
      const known = entry.enables.find((one) => one[0] === method);
      if (known) known[1] = params;
      else entry.enables.push([method, params]);
    } else if (ENABLE_SHAPE.test(method.replace(/\.disable$/, ".enable"))) {
      const enable = method.slice(0, -".disable".length) + ".enable";
      entry.enables = entry.enables.filter(([one]) => one !== enable);
    }
    if (method === "Emulation.setDeviceMetricsOverride") return this.emulate(entry, params);
    if (method === "Emulation.clearDeviceMetricsOverride") return this.emulate(entry, null);
    try {
      params = scaleInput(method, params, entry.scale);
    } catch (e) {
      return Promise.reject(e);
    }
    // A capture puts the page's emulation back without its scale.
    if (method === "Page.captureScreenshot") return this.command(entry, method, params).finally(() => void this.fit(entry, true));
    if (!method.startsWith("Input.")) return this.command(entry, method, params);
    entry.synthUntil = Date.now() + 250;
    return this.inputWake.run(() => this.command(entry, method, params));
  }

  // command is one debugger call the kernel is waiting on. A session that dies
  // never settles its calls on its own, so a sleep fails them here: the kernel
  // reads an error it can retry, not a timeout it cannot tell from a vanishing.
  command(entry, method, params) {
    return new Promise((resolve, reject) => {
      entry.pending.add(reject);
      entry.view.webContents.debugger.sendCommand(method, params).then(
        (value) => {
          entry.pending.delete(reject);
          resolve(value);
        },
        (err) => {
          entry.pending.delete(reject);
          reject(err);
        },
      );
    });
  }

  // sleep puts a page away whole: its renderer is closed, and what is kept is
  // the address it was on and a picture to draw while it is gone. Nothing the
  // kernel holds changes, so its handle survives and the next call wakes it.
  async sleep(entry) {
    if (entry.slept || entry.closed || !this.maySleep(entry)) return;
    const contents = entry.view && entry.view.webContents;
    if (!contents) return;
    // The picture is what the window draws while the page is gone, taken the
    // way freeze() takes its own; a page that cannot be photographed is slept
    // all the same.
    let url = entry.url;
    let picture = "";
    try {
      url = contents.getURL() || entry.url;
      const image = await contents.capturePage();
      if (!image.isEmpty()) picture = "data:image/jpeg;base64," + image.toJPEG(85).toString("base64");
    } catch {
      picture = "";
    }
    // The page may have been shown, woken, closed, or begun a navigation while
    // the picture was taken; a sleep that lost that race must not tear anything
    // down, and a navigation killed mid-flight hangs the kernel waiting on it.
    if (entry.slept || entry.closed || !entry.view || !this.maySleep(entry)) return;
    entry.slept = { url, picture };
    // The calls still open on the session that is about to die are closed
    // first, as errors: the kernel hears nothing of a sleep, so no one else
    // will tell them.
    for (const fail of entry.pending) fail(new Error("the page went to sleep"));
    entry.pending.clear();
    entry.sleeping = true;
    if (entry.attached && !this.win.isDestroyed()) this.win.contentView.removeChildView(entry.view);
    entry.attached = false;
    if (!contents.isDestroyed()) contents.close();
    entry.view = null;
    entry.at = null;
    entry.panel = null;
    entry.override = null;
    entry.scale = 1;
    // A settle timer left running would put a view back that no longer
    // exists, or worse, the one a wake builds next.
    clearTimeout(entry.returning);
    entry.returning = null;
    // The ceiling on the dead: a tombstone is only a picture and an address,
    // but a driver that never comes back leaves one for every page it opened.
    // Past this the oldest-touched is buried whole and the kernel is told.
    let tombs = 0;
    let oldest = null;
    for (const other of this.entries.values()) {
      if (!other.slept) continue;
      tombs++;
      if (!oldest || other.lastTouch < oldest.lastTouch) oldest = other;
    }
    if (tombs > SLEPT_LIMIT && oldest) oldest.close();
  }

  // wake raises a slept page: the view is built again on the address it dozed
  // off on, its viewport and the kernel's enabled domains are said again to the
  // new session, and the promise answers only once the page has loaded.
  wake(entry) {
    if (!entry.slept) return entry.rising || Promise.resolve();
    entry.url = entry.slept.url;
    entry.slept = null;
    // The sleep has landed, so whatever death its old contents still reports
    // is nothing for this one to answer for.
    entry.sleeping = false;
    const load = this.spawn(entry);
    // The recorded emulation is the view-fitting state a sleep threw away;
    // restoring it lets show() fit the panel the way it did before the sleep.
    entry.override = entry.emulated ? { ...entry.emulated } : null;
    for (const [method, params] of entry.enables) {
      entry.view.webContents.debugger.sendCommand(method, params).catch(() => {});
    }
    entry.rising = load.finally(() => {
      entry.rising = null;
    });
    return entry.rising;
  }

  // maySleep says whether a page may be put away: not while the person is looking
  // at it, a navigation or a settle is pending, a certificate or a dialog waits,
  // it is sounding, or DevTools is open. Downloads need no guard here.
  maySleep(entry) {
    if (entry.targetId === this.shown || entry.picking || entry.returning || entry.certificate || entry.dialog) return false;
    const contents = entry.view && entry.view.webContents;
    if (typeof contents?.isCurrentlyAudible === "function" && contents.isCurrentlyAudible()) return false;
    if (typeof contents?.isDevToolsOpened === "function" && contents.isDevToolsOpened()) return false;
    return true;
  }

  // The sweep puts pages nobody has touched to sleep. One page's failure must
  // not take the timer down with it: the pages after it are still paying.
  sweep() {
    const now = Date.now();
    for (const entry of this.entries.values()) {
      try {
        if (entry.slept || entry.sleeping) continue;
        if (now - entry.lastTouch <= this.sleepAfterMs) continue;
        if (!this.maySleep(entry)) continue;
        this.sleep(entry).catch(() => {});
      } catch {}
    }
  }

  // A preference saved while the app runs takes effect on the next sweep.
  setSleepAfterMs(ms) {
    this.sleepAfterMs = ms;
  }

  // What a test may read, and nothing more: whether a target is asleep, and
  // the address it is asleep on.
  asleep(targetId) {
    const entry = this.entries.get(targetId);
    return entry && entry.slept ? { url: entry.slept.url } : null;
  }

  // show draws one page over the rectangle the page reserved for it, in window
  // coordinates, and puts every other page away.
  show(targetId, rect) {
    const entry = this.entries.get(targetId);
    if (!entry || !validRect(rect)) return this.hide();
    entry.lastTouch = Date.now();
    // A slept page has no view to place, and the panel does not send its
    // rectangle twice: the show is retried once the wake has built the view,
    // and only once — two shows racing would both think they placed it.
    if (entry.slept) {
      if (!entry.showing) {
        entry.showing = true;
        this.wake(entry).then(
          () => {
            entry.showing = false;
            this.show(targetId, rect);
          },
          () => {
            entry.showing = false;
          },
        );
      }
      return;
    }
    const at = {
      x: Math.round(rect.x),
      y: Math.round(rect.y),
      width: Math.round(rect.width),
      height: Math.round(rect.height),
    };
    // The panel reports its rectangle whenever anything in the window changes,
    // nearly always the same one: re-adding the view and setting the bounds it
    // already has is work for every one of those.
    if (this.shown === targetId && sameRect(entry.panel, at)) return;
    this.placed++;
    for (const [id, other] of this.entries) {
      if (id !== targetId && other.view) this.putAway(other.view, other);
    }
    this.shown = targetId;
    // A page that was parked comes back as a view that is attached but whose
    // widget never repaints until the window's geometry changes; attaching it
    // again is what wakes it, the same as after a minimize.
    if (entry.attached && !entry.at) this.win.contentView.removeChildView(entry.view);
    this.win.contentView.addChildView(entry.view);
    entry.attached = true;
    entry.panel = at;
    this.fit(entry);
    this.keepKeyboard();
  }

  // fit puts the view where its page is drawn whole and tells the page how it is
  // scaled; a page parked out of sight is not scaled. force sends the scale again
  // though it has not changed, for a capture that put the page's emulation back.
  fit(entry, force = false) {
    if (!entry.panel) return Promise.resolve();
    const { at, scale } = placement(entry.panel, entry.override && { width: entry.override.width, height: entry.override.height });
    entry.view.setBounds(at);
    entry.at = at;
    return this.scaled(entry, scale, force);
  }

  scaled(entry, scale, force = false) {
    if (!entry.override || (entry.scale === scale && !force)) return Promise.resolve();
    entry.scale = scale;
    return this.override(entry).catch(() => {});
  }

  override(entry) {
    return entry.view.webContents.debugger.sendCommand("Emulation.setDeviceMetricsOverride", { ...entry.override, scale: entry.scale });
  }

  // emulate records the viewport a page was told to lay out in, which the view
  // is then fitted to; null is the page going back to the size of its view.
  // The recording is what survives a sleep and comes back with the page.
  async emulate(entry, params) {
    entry.override = sized(params) ? { ...params } : null;
    entry.emulated = sized(params) ? { ...params } : null;
    if (!entry.override) {
      entry.scale = 1;
      if (entry.panel) this.fit(entry);
      return entry.view.webContents.debugger.sendCommand("Emulation.clearDeviceMetricsOverride", {});
    }
    entry.scale = 1;
    if (entry.panel) {
      const { at, scale } = placement(entry.panel, entry.override);
      entry.view.setBounds(at);
      entry.at = at;
      entry.scale = scale;
    }
    return this.override(entry);
  }

  // Attaching a view can leave nobody holding the keyboard, and the platform
  // settles that a moment later, so the page is handed it back twice.
  keepKeyboard() {
    this.keyboard.reclaim();
    setTimeout(() => this.keyboard.reclaim(), 150);
  }

  reattach() {
    for (const [id, entry] of this.entries) {
      if (this.win.isDestroyed() || !entry.view || entry.view.webContents.isDestroyed() || !entry.attached) continue;
      this.win.contentView.removeChildView(entry.view);
      this.win.contentView.addChildView(entry.view);
      if (id !== this.shown) this.putAway(entry.view, entry);
    }
    this.keepKeyboard();
  }

  // A navigation that starts in an attached view takes the keyboard from the app
  // page and would commit a composition there as raw letters. While the app page
  // holds the keyboard the view is held out of the window until it ends.
  leave(entry) {
    if (!entry.attached || this.win.isDestroyed() || this.keyboard.owner !== "app") return;
    this.win.contentView.removeChildView(entry.view);
    entry.attached = false;
    clearTimeout(entry.returning);
    entry.returning = setTimeout(() => this.settle(entry), LEAVE_LIMIT_MS);
  }

  settle(entry) {
    clearTimeout(entry.returning);
    entry.returning = undefined;
    if (entry.attached || !entry.view || this.win.isDestroyed() || entry.view.webContents.isDestroyed()) return;
    this.win.contentView.addChildView(entry.view);
    entry.attached = true;
    if (entry.at) entry.view.setBounds(entry.at);
    else this.putAway(entry.view, entry);
  }

  // freeze answers with a picture of the page on screen, taken before it is put
  // away. Only the page already shown is ever taken, and the picture goes to
  // this window's page and nowhere else.
  async freeze() {
    const id = this.shown;
    const entry = this.entries.get(id);
    // A slept page was photographed when it went to sleep and has no view to
    // take another with: its picture is the answer, and there is nothing to
    // put away.
    if (entry && entry.slept) return entry.slept.picture;
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
    for (const entry of this.entries.values()) {
      if (entry.view) this.putAway(entry.view, entry);
    }
    // A pick behind a page nobody can see cannot be used: the person is looking
    // at a picture, so the pick is settled instead of swallowing clicks unseen.
    for (const entry of this.entries.values()) {
      if (!entry.picking) continue;
      entry.picking.reason = "hidden";
      void this.cancelPick(entry.targetId);
    }
  }

  putAway(view, entry) {
    const { height } = this.win.getContentBounds();
    view.setBounds({ x: 1 - AGENT_VIEWPORT.width, y: height - 1, ...AGENT_VIEWPORT });
    if (entry) {
      entry.at = null;
      entry.panel = null;
      void this.scaled(entry, 1);
    }
  }

  control(targetId, action) {
    const entry = this.entries.get(targetId);
    const view = entry?.view;
    if (!view) {
      // A slept page has no contents to steer; waking it first is what makes
      // the action land on the page it comes back as, instead of nowhere.
      if (entry?.slept) this.wake(entry).then(() => this.control(targetId, action)).catch(() => {});
      return;
    }
    const contents = view.webContents;
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

  // pick arms one page's element picker and answers what the person pointed
  // at, or { cancelled: true } when they pressed Escape, navigated away, or
  // left it longer than a pick may wait. The highlight and the hover card are
  // drawn inside the page: a native view sits above anything this window's own
  // page could draw over it.
  // The page wrote the answer, so the shell decides what an answer is: fields
  // from an allow-list, strings clipped to the page script's own limits, and a
  // shape it cannot read treated as gone.
  clean(answer) {
    const gone = { cancelled: true, reason: "gone" };
    if (!answer || typeof answer !== "object") return gone;
    if (answer.cancelled) {
      const reason = CANCEL_REASONS.includes(answer.reason) ? answer.reason : "gone";
      return { cancelled: true, reason };
    }
    const el = answer.element;
    if (!el || typeof el !== "object") return gone;
    const out = {};
    for (const field of ELEMENT_STRINGS) {
      const value = el[field];
      if (typeof value !== "string") continue;
      out[field] = clip(value, field === "htmlExcerpt" ? MARKUP_CLIP : field === "nearbyText" ? NEARBY_CLIP : TEXT_CLIP);
    }
    if (SELECTOR_BASIS.includes(el.selectorBasis)) out.selectorBasis = el.selectorBasis;
    if (el.attributes && typeof el.attributes === "object") {
      const attributes = {};
      for (const [key, value] of Object.entries(el.attributes)) {
        if (Object.keys(attributes).length >= ATTR_LIMIT) break;
        if (typeof value === "string") attributes[clip(key, VALUE_CLIP)] = clip(value, VALUE_CLIP);
      }
      out.attributes = attributes;
    }
    if (Number.isFinite(el.capturedAt)) out.capturedAt = el.capturedAt;
    if (el.rect && typeof el.rect === "object") {
      const { x, y, width, height } = el.rect;
      if ([x, y, width, height].every(Number.isFinite)) out.rect = { x, y, width, height };
    }
    if (el.style && typeof el.style === "object") {
      out.style = {};
      for (const key of STYLE_KEYS) if (typeof el.style[key] === "string") out.style[key] = clip(el.style[key], STYLE_CLIP);
    }
    if (!out.selector || !out.tagName) return gone;
    return { cancelled: false, element: out };
  }

  async pick(targetId, labels) {
    const entry = this.entries.get(targetId);
    if (!entry) return { cancelled: true, reason: "gone" };
    // A pick draws inside the page that is on screen; a page that is not shown
    // would pick behind the one the person is looking at.
    if (entry.targetId !== this.shown) return { cancelled: true, reason: "hidden" };
    if (entry.slept) await this.wake(entry).catch(() => {});
    const contents = entry.view && entry.view.webContents;
    if (!contents || contents.isDestroyed()) return { cancelled: true, reason: "gone" };
    // A second pick on the same page replaces the first: one picker, one
    // overlay, and the older call answers as cancelled.
    if (entry.picking) {
      entry.picking.reason = "dismissed";
      await this.cancelPick(targetId).catch(() => {});
    }
    // Why the pick ends when it ends without an element: Escape and a dismissal
    // come from the page, the other three from here. The person is told which,
    // so "nothing happened" is never the answer.
    const picking = (entry.picking = { reason: "" });
    const timer = setTimeout(() => {
      picking.reason = "timeout";
      void this.cancelPick(targetId);
    }, PICK_LIMIT_MS);
    if (typeof timer.unref === "function") timer.unref();
    try {
      const answer = this.clean(await contents.executeJavaScript(ARM(labels), false));
      if (answer.cancelled) return picking.reason ? { cancelled: true, reason: picking.reason } : answer;
      // The page cannot invent an element: the shell re-finds the selector in
      // its own world before the answer is trusted.
      if (!(await this.lands(contents, answer.element))) return { cancelled: true, reason: "gone" };
      // The picture is what the person saw: a complaint about how something
      // looks is answered by the element, not only by its markup.
      const shot = await this.shot(entry, answer.element.rect).catch(() => "");
      if (shot) answer.shot = shot;
      return answer;
    } catch {
      // The page navigated, reloaded, slept, or was closed under the pick.
      return { cancelled: true, reason: picking.reason || "navigation" };
    } finally {
      if (entry.picking === picking) entry.picking = null;
      clearTimeout(timer);
    }
  }

  // lands asks the DOM from the shell's own world whether a selector still
  // names an element of the tag the pick reported. A page script cannot answer
  // this one in the page's place.
  async lands(contents, element) {
    if (!element || !element.selector || !element.tagName) return false;
    const tag = await contents
      .executeJavaScriptInIsolatedWorld(PICK_WORLD, [
        {
          code: `(() => {
            const el = document.querySelector(${JSON.stringify(element.selector)});
            return el ? el.tagName.toLowerCase() : "";
          })()`,
        },
      ])
      .catch(() => "");
    return tag === String(element.tagName).toLowerCase();
  }

  // shot captures the element as the view drew it. The page's rect is in CSS
  // pixels and the view may be fitted, so the scale is applied and the result
  // is kept inside the view; an element scrolled out of sight has no picture.
  async shot(entry, rect) {
    const contents = entry.view && entry.view.webContents;
    if (!contents || contents.isDestroyed() || !rect) return "";
    const scale = entry.scale || 1;
    const bounds = entry.view.getBounds();
    const at = {
      x: Math.max(0, Math.round(rect.x * scale)),
      y: Math.max(0, Math.round(rect.y * scale)),
      width: Math.round(rect.width * scale),
      height: Math.round(rect.height * scale),
    };
    at.width = Math.min(at.width, bounds.width - at.x);
    at.height = Math.min(at.height, bounds.height - at.y);
    if (at.width < 1 || at.height < 1) return "";
    const image = await contents.capturePage(at);
    return image.isEmpty() ? "" : image.toDataURL();
  }

  // verify re-finds a selector after the page changed, so a person can see
  // whether an edit landed where the pick pointed.
  async verify(targetId, selector) {
    const entry = this.entries.get(targetId);
    if (!entry || !selector) return { found: false };
    const contents = entry.view && entry.view.webContents;
    if (!contents || contents.isDestroyed()) return { found: false };
    const at = await contents
      .executeJavaScriptInIsolatedWorld(PICK_WORLD, [
        {
          code: `(() => {
            const el = document.querySelector(${JSON.stringify(selector)});
            if (!el) return { found: false };
            const r = el.getBoundingClientRect();
            return { found: true, tag: el.tagName.toLowerCase(), rect: { x: r.x, y: r.y, width: r.width, height: r.height } };
          })()`,
        },
      ])
      .catch(() => ({ found: false }));
    if (!at || !at.found || !at.rect) return { found: false };
    const shot = await this.shot(entry, at.rect).catch(() => "");
    return { found: true, tag: at.tag, rect: at.rect, shot };
  }

  // cancelPick disarms the picker, which settles a pick still waiting on the
  // page. A page that has gone answers nothing, which reads as cancelled.
  cancelPick(targetId) {
    const entry = this.entries.get(targetId);
    const contents = entry && entry.view && entry.view.webContents;
    if (!contents || contents.isDestroyed()) return Promise.resolve(false);
    return contents.executeJavaScript(STOP, false).then(
      () => true,
      () => false,
    );
  }

  // navigate starts loading what the person typed and answers "", or the code
  // of why the window will not: "scheme" or "network_file".
  navigate(targetId, address) {
    const entry = this.entries.get(targetId);
    const { url, fallback, refusal } = typed(address, this.kernelOrigin);
    if (!entry) return "scheme";
    if (refusal) return refusal;
    // An address typed at a slept page is for the page it wakes up as.
    if (entry.slept) {
      this.wake(entry).then(() => this.loadTyped(targetId, entry, url, fallback)).catch(() => {});
      return "";
    }
    void this.loadTyped(targetId, entry, url, fallback);
    return "";
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
      if (!details.isMainFrame || details.isSameDocument) return;
      this.report(targetId, null);
      this.leave(entry);
    });
    contents.on("did-stop-loading", () => this.settle(entry));
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
    // The page the certificate was refused for may be asleep by now.
    if (entry.slept) {
      this.wake(entry).then(() => this.loadTyped(targetId, entry, cert.url, "")).catch(() => {});
      return true;
    }
    void this.loadTyped(targetId, entry, cert.url, "");
    return true;
  }

  send(channel, payload) {
    if (!this.win.isDestroyed()) this.win.webContents.send(channel, payload);
  }

  closeAll() {
    // The sweep's timer must not outlive the pages it sweeps: the pages go,
    // and a timer left holding the process open keeps a test run from ever
    // ending.
    if (this.sweeper) {
      clearInterval(this.sweeper);
      this.sweeper = null;
    }
    for (const id of [...this.logins.keys()]) this.answerLogin(id);
    for (const entry of [...this.entries.values()]) entry.close();
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

// Where a page's view sits in the rectangle the panel gave it. A page laid out
// in a viewport of its own is shown whole: scaled down to fit and never up,
// centred, with the view sized to exactly what it draws.
const sized = (v) => !!v && Number.isFinite(v.width) && Number.isFinite(v.height) && v.width > 0 && v.height > 0;

function placement(panel, emulated) {
  if (!sized(emulated)) return { at: panel, scale: 1 };
  const scale = Math.min(1, panel.width / emulated.width, panel.height / emulated.height);
  const width = Math.max(1, Math.round(emulated.width * scale));
  const height = Math.max(1, Math.round(emulated.height * scale));
  const x = panel.x + Math.round((panel.width - width) / 2);
  const y = panel.y + Math.round((panel.height - height) / 2);
  return { at: { x, y, width, height }, scale };
}

// Input that carries a point lands where it is drawn only after the same scaling
// the view was drawn at. The mouse is measured to; a point that lands elsewhere
// silently is worse than a refusal, so the rest are refused while scaled.
const POINTED_INPUT = new Set([
  "Input.dispatchTouchEvent",
  "Input.dispatchDragEvent",
  "Input.emulateTouchFromMouseEvent",
  "Input.synthesizeTapGesture",
  "Input.synthesizeScrollGesture",
  "Input.synthesizePinchGesture",
]);

function scaleInput(method, params, scale) {
  if (scale === 1) return params;
  if (method === "Input.dispatchMouseEvent") return { ...params, x: params.x * scale, y: params.y * scale };
  if (POINTED_INPUT.has(method)) {
    throw Object.assign(new Error(`${method} is not available while the page is scaled to fit the panel`), { code: "input_unscaled" });
  }
  return params;
}

function sameRect(a, b) {
  return !!a && a.x === b.x && a.y === b.y && a.width === b.width && a.height === b.height;
}

function validRect(rect) {
  return rect && [rect.x, rect.y, rect.width, rect.height].every(Number.isFinite) && rect.width > 0 && rect.height > 0;
}

module.exports = { BrowserViews, AGENT_VIEWPORT, LEAVE_LIMIT_MS, placement, scaleInput };
