"use strict";
// Drives the element picker in the real shell: a person's own mouse and keys
// over a page of our own, in a user-data directory of our own. Run with
// `pnpm pick-live`; it needs no kernel and no network.
const http = require("node:http");
const os = require("node:os");
const path = require("node:path");
const fs = require("node:fs");
const { app, BrowserWindow } = require("electron");
const { BrowserViews } = require("../src/browserviews");

// Nothing this test writes may land in the Studio's own profile: a run that
// picked a password field must not leave that page cached in anyone's session.
app.setPath("userData", fs.mkdtempSync(path.join(os.tmpdir(), "rx-pick-live-")));

const wait = (ms) => new Promise((r) => setTimeout(r, ms));
const fails = [];
const check = (name, ok, got) => {
  console.log((ok ? "ok   " : "FAIL ") + name + (ok ? "" : " — " + JSON.stringify(got)));
  if (!ok) fails.push(name);
};

const GIF = "data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7";
const PAGE = `<!doctype html><html><head><meta charset="utf-8"><title>Pick lab</title>
<style>
  body { margin: 0; font: 16px sans-serif; }
  #hero { position: absolute; left: 40px; top: 60px; width: 300px; height: 120px; background: #c0392b; color: #fff; }
  #logo { width: 40px; height: 20px; margin: 10px; }
  #go { position: absolute; left: 40px; top: 220px; }
  #pw { position: absolute; left: 40px; top: 270px; }
</style></head><body>
<div id="hero">Hero <img id="logo" alt="logo" src="${GIF}"></div>
<button id="go" class="btn__a1b2c3" data-testid="go-button" aria-label="投放" onclick="window.__clicked = true">Go</button>
<label>Password <input id="pw" type="password" value="secret-value"></label>
<script>window.__enter = false; addEventListener("keydown", (event) => { if (event.key === "Enter") window.__enter = true; });</script>
<h1 id="head" style="position:absolute;left:40px;top:340px">A heading long enough to carry its own words and ask for no page context around it</h1>
<span id="dot" style="position:absolute;left:40px;top:420px;width:7px;height:7px;background:#2ecc71"></span>
<div id="badge" class="badge__x9y8z7" aria-label="状态徽标" style="position:absolute;left:40px;top:470px">ok</div>
<em class="em__a1b2c3" style="position:absolute;left:40px;top:520px">hash</em>
<iframe id="frame" srcdoc="&lt;p&gt;inside&lt;/p&gt;" style="position:absolute;left:40px;top:570px;width:120px;height:60px"></iframe>
<div id="shadow" style="position:absolute;left:200px;top:570px;width:80px;height:40px"></div>
<script>document.getElementById("shadow").attachShadow({ mode: "open" }).innerHTML = "<span>inside shadow</span>";</script>
</body></html>`;

app.whenReady().then(async () => {
  const server = http.createServer((_q, s) => {
    s.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
    s.end(PAGE);
  });
  await new Promise((r) => server.listen(0, "127.0.0.1", r));
  const origin = `http://127.0.0.1:${server.address().port}/`;

  const win = new BrowserWindow({ width: 1400, height: 1000, show: true, webPreferences: { sandbox: true } });
  await win.loadURL("data:text/html,<body style='margin:0'><p>studio page</p></body>");
  const views = new BrowserViews({ win, kernelOrigin: "http://127.0.0.1:1" });
  let loaded;
  const ready = new Promise((r) => (loaded = r));
  const page = views.create({
    partition: "pick-live",
    url: origin,
    onEvent: (method) => method === "Page.loadEventFired" && loaded(),
    onClosed() {},
    onPopup() {},
    onDownload() {},
  });
  await page.send("Page.enable");
  await Promise.race([ready, wait(4000)]);
  const view = views.entries.get(page.targetId).view;
  view.webContents.on("console-message", (_event, _level, message) => console.log("page console:", message));
  views.show(page.targetId, { x: 0, y: 0, width: 1280, height: 900 });
  await wait(600);
  // Input only reaches a window the platform considers live; a background
  // window drops the pressed half of a click while still delivering moves.
  win.show();
  win.focus();
  await wait(300);
  view.webContents.focus();

  const evalIn = (expression) => page.send("Runtime.evaluate", { expression, returnByValue: true }).then((r) => r.result.value);
  const at = (id) =>
    evalIn(`(() => { const b = document.getElementById(${JSON.stringify(id)}).getBoundingClientRect();
      return { x: b.x + b.width / 2, y: b.y + b.height / 2, width: b.width, height: b.height }; })()`);
  const atSel = (selector) =>
    evalIn(`(() => { const b = document.querySelector(${JSON.stringify(selector)}).getBoundingClientRect();
      return { x: b.x + b.width / 2, y: b.y + b.height / 2, width: b.width, height: b.height }; })()`);
  const move = (point) => page.send("Input.dispatchMouseEvent", { type: "mouseMoved", x: point.x, y: point.y, button: "none", clickCount: 0 });
  const click = async (point) => {
    await move(point);
    // Let the hover settle before the press: a new pick arms a frame later.
    await wait(80);
    // A native input event rather than a CDP press: the page half is what this
    // guards, and the window's own input path is what a person's click takes.
    const x = Math.round(point.x);
    const y = Math.round(point.y);
    view.webContents.sendInputEvent({ type: "mouseDown", x, y, button: "left", clickCount: 1 });
    view.webContents.sendInputEvent({ type: "mouseUp", x, y, button: "left", clickCount: 1 });
  };
  const overlays = () =>
    evalIn(`[...document.querySelectorAll('[data-reasonix-pick="box"], [data-reasonix-pick="card"]')].map((el) => ({
      kind: el.dataset.reasonixPick, text: el.innerText, shown: getComputedStyle(el).display !== "none" }))`);

  await evalIn("window.__clicked = false");

  // 1. A person points at the image: the highlight and the card are drawn in
  // the page, and what comes back is the element and where it sits.
  const picking = views.pick(page.targetId);
  await wait(200);
  const logo = await at("logo");
  await move(logo);
  await wait(150);
  const drawn = await overlays();
  check("hovering draws the highlight and the card inside the page", drawn.length === 2 && drawn.every((one) => one.shown), drawn);
  check(
    "the card names the colour and the font, and adds no background for a transparent one",
    /Color/.test(drawn[1].text) && /Font/.test(drawn[1].text) && !/Background/.test(drawn[1].text),
    drawn[1].text,
  );
  await click(logo);
  const picked = await picking;
  check("a click answers with the element under the pointer", picked.cancelled === false && picked.element.tagName === "img", picked);
  check("the element comes with a picture of itself", /^data:image\/png/.test(picked.shot || ""), (picked.shot || "").slice(0, 24));
  check("the selector names it again", picked.element.selector === "#logo", picked.element.selector);
  check(
    "the box is the one the page laid out",
    Math.abs(picked.element.rect.width - logo.width) <= 1 && Math.abs(picked.element.rect.height - logo.height) <= 1,
    { rect: picked.element.rect, logo },
  );
  check("it carries the page it came from", picked.element.pageUrl === origin, picked.element.pageUrl);
  check("an element with little text of its own gets its container's", picked.element.nearbyText === "Hero", picked.element.nearbyText);
  check("the card is put away once the pick settles", (await overlays()).length === 0, await overlays());

  // 2. A pick is not a click for the page: the button's own handler never runs.
  const buttonPick = views.pick(page.targetId);
  await wait(200);
  await click(await at("go"));
  const button = await buttonPick;
  check("the page's own click handler does not run", (await evalIn("window.__clicked")) === false, await evalIn("window.__clicked"));
  check(
    "a button says what it is and what it is called",
    button.element.tagName === "button" && button.element.role === "button" && button.element.accessibleName === "投放",
    button.element,
  );
  check("a button with its own word asks for no context", button.element.nearbyText === undefined, button.element.nearbyText);

  // 3. What a person typed into a password field never leaves the page.
  const passwordPick = views.pick(page.targetId);
  await wait(200);
  await click(await at("pw"));
  const password = await passwordPick;
  check("a password field answers with a placeholder, not its value", password.element.text === "[masked password input]", password.element.text);
  check("neither the markup nor the attributes carry what was typed", !JSON.stringify(password.element).includes("secret-value"), password.element.htmlExcerpt);

  // 4. A heading says what it is already: the nearest article around it is the
  // whole page, and that must not be handed over as context.
  const headingPick = views.pick(page.targetId);
  await wait(200);
  await click(await at("head"));
  const heading = await headingPick;
  check("a heading carries no page-wide context", heading.element.nearbyText === undefined, heading.element.nearbyText);

  // 5. A decorative mark has no words and no role: nothing around it makes it
  // meaningful, so no context comes along either, and the fields that have no
  // answer are left out instead of sent empty.
  const dotPick = views.pick(page.targetId);
  await wait(200);
  await click(await at("dot"));
  const dot = await dotPick;
  check(
    "a decorative mark carries no context and no empty fields",
    dot.element.nearbyText === undefined && dot.element.role === undefined && dot.element.text === undefined,
    dot.element,
  );

  // 6. The page's own identity wins over its styling: a hash class is gone
  // after the next deploy, a data-testid is not.
  const identity = views.pick(page.targetId);
  await wait(200);
  await click(await at("go"));
  const byTestId = await identity;
  check(
    "a data-testid is the selector when the page has one",
    byTestId.element.selector === '[data-testid="go-button"]' && byTestId.element.selectorBasis === "testid",
    byTestId.element,
  );
  check("the hash class behind it is not used", !byTestId.element.selector.includes("btn__a1b2c3"), byTestId.element.selector);

  const labelled = views.pick(page.targetId);
  await wait(200);
  await click(await at("badge"));
  const byLabel = await labelled;
  check(
    "an aria-label is the next choice",
    byLabel.element.selector === 'div[aria-label="状态徽标"]' && byLabel.element.selectorBasis === "aria-label",
    byLabel.element,
  );

  const styled = views.pick(page.targetId);
  await wait(200);
  await click(await atSel(".em__a1b2c3"));
  const byClass = await styled;
  check(
    "with neither, the styling class is used and labelled as such",
    byClass.element.selectorBasis === "class" && byClass.element.selector.includes("em__a1b2c3"),
    byClass.element,
  );
  check(
    "and it still finds the element",
    (await evalIn(`document.querySelector(${JSON.stringify(byClass.element.selector)})?.tagName`)) === "EM",
    byClass.element.selector,
  );

  // 7. Stepping up with the arrow key: what the card shows and what comes back
  // are the same level.
  const climbing = views.pick(page.targetId);
  await wait(200);
  const logoPoint = await at("logo");
  await move(logoPoint);
  await wait(100);
  const boxWidth = () => evalIn(`parseFloat(document.querySelector('[data-reasonix-pick="box"]').style.width)`);
  check("the pointer starts on the image", (await boxWidth()) === 40, await boxWidth());
  view.webContents.sendInputEvent({ type: "keyDown", keyCode: "Up" });
  await wait(120);
  view.webContents.sendInputEvent({ type: "keyUp", keyCode: "Up" });
  check("one step up is the hero", (await boxWidth()) === 300, await boxWidth());
  const chainText = await evalIn(`document.querySelector('[data-reasonix-pick="card"]').innerText`);
  check("the card shows the chain it climbed", /div#hero/.test(chainText) && !/img/.test(chainText), chainText);
  // Pressed without moving: a mousemove would put the pointer back on the image.
  const climbX = Math.round(logoPoint.x);
  const climbY = Math.round(logoPoint.y);
  view.webContents.sendInputEvent({ type: "mouseDown", x: climbX, y: climbY, button: "left", clickCount: 1 });
  view.webContents.sendInputEvent({ type: "mouseUp", x: climbX, y: climbY, button: "left", clickCount: 1 });
  const climbed = await climbing;
  check("the answer is the level stepped up to", climbed.element.tagName === "div" && climbed.element.selector === "#hero", climbed.element);

  // 9. A frame or a shadow host is named, not guessed at.
  const framePick = views.pick(page.targetId);
  await wait(200);
  await move(await at("frame"));
  await wait(100);
  const frameNote = await evalIn(`document.querySelector('[data-reasonix-pick="card"]').innerText`);
  check("an embedded frame says why it cannot be picked", /embedded frame/.test(frameNote), frameNote);
  await click(await at("frame"));
  await wait(300);
  const stillArmed = await evalIn(`!!document.querySelector('[data-reasonix-pick="box"]')`);
  check("clicking the frame leaves the pick armed", stillArmed === true, stillArmed);
  void views.cancelPick(page.targetId);
  const frameAnswer = await framePick;
  check("and the frame is not reported as an element", frameAnswer.cancelled === true, frameAnswer);

  const shadowPick = views.pick(page.targetId);
  await wait(200);
  await move(await at("shadow"));
  await wait(100);
  const shadowNote = await evalIn(`document.querySelector('[data-reasonix-pick="card"]').innerText`);
  check("a shadow host says why it cannot be picked", /shadow root/.test(shadowNote), shadowNote);
  void views.cancelPick(page.targetId);
  await shadowPick;

  // 10. A second pick on the same page replaces the first instead of stacking.
  const firstPick = views.pick(page.targetId);
  await wait(200);
  const secondPick = views.pick(page.targetId);
  await wait(300);
  const draws = await evalIn(
    `document.querySelectorAll('[data-reasonix-pick="box"]').length + "/" + document.querySelectorAll('[data-reasonix-pick="card"]').length`,
  );
  check("a second pick leaves one picker on the page", draws === "1/1", draws);
  const firstAnswer = await firstPick;
  check("the first pick answers as cancelled", firstAnswer.cancelled === true, firstAnswer);
  void views.cancelPick(page.targetId);
  await secondPick;

  // 11. Shift-click keeps a set: the click that ends it carries them all.
  const multi = views.pick(page.targetId);
  await wait(200);
  const goAt = await at("go");
  await move(goAt);
  await wait(100);
  view.webContents.sendInputEvent({ type: "mouseDown", x: Math.round(goAt.x), y: Math.round(goAt.y), button: "left", clickCount: 1, modifiers: ["shift"] });
  view.webContents.sendInputEvent({ type: "mouseUp", x: Math.round(goAt.x), y: Math.round(goAt.y), button: "left", clickCount: 1, modifiers: ["shift"] });
  await wait(150);
  const marked = await evalIn(`document.querySelectorAll("[data-reasonix-picked]").length`);
  check("shift-click keeps the element marked", marked === 1, marked);
  await click(await at("badge"));
  const many = await multi;
  check("the set comes back as several elements", many.elements?.length === 2, many.elements?.map((e) => e.tagName));

  // 12. Verifying the selector after the page changed.
  const still = await views.verify(page.targetId, "#logo");
  check("a selector that still exists comes back with a fresh picture", still.found === true && /^data:image\/png/.test(still.shot || ""), still);
  const gone = await views.verify(page.targetId, "#nope");
  check("a selector that is gone says so", gone.found === false, gone);

  // 8. The keyboard is the picker's too while it is armed: Enter must not press
  // a focused button, and Escape is the one key that ends the pick — saying so.
  const escaping = views.pick(page.targetId);
  await wait(200);
  await page.send("Input.dispatchKeyEvent", { type: "keyDown", key: "Enter", code: "Enter", windowsVirtualKeyCode: 13, nativeVirtualKeyCode: 13 });
  await page.send("Input.dispatchKeyEvent", { type: "keyUp", key: "Enter", code: "Enter", windowsVirtualKeyCode: 13, nativeVirtualKeyCode: 13 });
  check("Enter does not reach the page", (await evalIn("window.__enter")) === false, await evalIn("window.__enter"));
  await page.send("Input.dispatchKeyEvent", { type: "keyDown", key: "Escape", code: "Escape", windowsVirtualKeyCode: 27, nativeVirtualKeyCode: 27 });
  const escaped = await escaping;
  check("Escape cancels the pick, and says why", escaped.cancelled === true && escaped.reason === "escape", escaped);
  check("and leaves nothing drawn", (await overlays()).length === 0);

  // 7. A page put away under the picker ends the pick instead of swallowing
  // the person's clicks on a picture they cannot reach.
  const hiding = views.pick(page.targetId);
  await wait(200);
  views.hide();
  const hidden = await hiding;
  check("a page put away cancels the pick, and says why", hidden.cancelled === true && hidden.reason === "hidden", hidden);

  views.hide();
  page.close();
  server.close();
  console.log(fails.length ? `${fails.length} failed` : "all checks passed");
  app.exit(fails.length ? 1 : 0);
});
