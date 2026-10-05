"use strict";
// Freezes a real page in the real shell: the picture the window draws while its
// own layer covers the page must be that page, taken before it is put away.
// Run with `pnpm freeze-live`; it needs no kernel and no network.
const { app, BrowserWindow, nativeImage } = require("electron");
const { BrowserViews } = require("../src/browserviews");
const wait = (ms) => new Promise((r) => setTimeout(r, ms));
const fails = [];
const check = (name, ok, got) => {
  console.log((ok ? "ok   " : "FAIL ") + name + (ok ? "" : " — " + JSON.stringify(got)));
  if (!ok) fails.push(name);
};
app.whenReady().then(async () => {
  const win = new BrowserWindow({ width: 900, height: 700, show: true, webPreferences: { sandbox: true } });
  await win.loadURL("data:text/html,<body style='margin:0;background:%23eee'><h3>studio page</h3></body>");
  const views = new BrowserViews({ win, kernelOrigin: "http://127.0.0.1:1" });
  let loaded;
  const ready = new Promise((r) => (loaded = r));
  const page = views.create({
    partition: "freeze-live",
    url: "data:text/html,<body style='margin:0;background:%23c0392b;color:white;font:40px sans-serif'><p>guest page</p></body>",
    onEvent: (method) => method === "Page.loadEventFired" && loaded(),
    onClosed() {},
    onPopup() {},
    onDownload() {},
  });
  await page.send("Page.enable");
  await Promise.race([ready, wait(3000)]);
  const rect = { x: 100, y: 120, width: 600, height: 400 };
  views.show(page.targetId, rect);
  await wait(600);
  const view = views.entries.get(page.targetId).view;
  check("shown at the panel's rectangle", JSON.stringify(view.getBounds()) === JSON.stringify(rect), view.getBounds());
  const picture = await views.freeze();
  check("freeze answers with a jpeg of the page", picture.startsWith("data:image/jpeg;base64,") && picture.length > 1000, picture.slice(0, 40));
  const b = view.getBounds();
  check("the page is put away after the picture is taken", b.x < 0 && views.shown === "", { b, shown: views.shown });
  const img = nativeImage.createFromDataURL(picture);
  const bmp = img.toBitmap();
  const size = img.getSize();
  const mid = ((size.height >> 1) * size.width + (size.width >> 1)) * 4;
  check("the picture is the guest page's own pixels", bmp[mid + 2] > 150 && bmp[mid + 1] < 100, [bmp[mid + 2], bmp[mid + 1], bmp[mid]]);
  // A show that lands while a freeze is still taking its picture wins.
  views.show(page.targetId, rect);
  await wait(300);
  const racing = views.freeze();
  views.show(page.targetId, { ...rect, width: 601 });
  await racing;
  check("a freeze overtaken by a show leaves the page shown", views.shown === page.targetId && view.getBounds().x === 100, { shown: views.shown, b: view.getBounds() });
  views.hide();
  check("with nothing shown, freeze answers with no picture", (await views.freeze()) === "", "");
  page.close();
  console.log(fails.length ? `${fails.length} failed` : "all checks passed");
  app.exit(fails.length ? 1 : 0);
});
