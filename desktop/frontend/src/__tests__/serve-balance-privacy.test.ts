import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { setImmediate } from "node:timers/promises";
import { test, type TestContext } from "node:test";
import { JSDOM } from "jsdom";

const html = readFileSync(new URL("../../../../internal/serve/index.html", import.meta.url), "utf8");
const storageKey = "reasonix.hideBalance";

function page(t: TestContext, saved = new Map<string, string>(), blocked = false, lang = "en") {
  const dom = new JSDOM(html.replace("'__LANG__'", `'${lang}'`), {
    url: "http://localhost:8787", runScripts: "outside-only",
  });
  t.after(() => dom.window.close());
  const { window } = dom;
  let balance: string | null = "¥82.97";
  let requests = 0;
  let poll: () => void = () => { throw new Error("Status polling was not registered"); };
  Object.defineProperty(window, "localStorage", { value: {
    getItem(key: string) { if (blocked) throw new Error("Storage blocked"); return saved.get(key) ?? null; },
    setItem(key: string, value: string) { if (blocked) throw new Error("Storage blocked"); saved.set(key, value); },
  } });
  window.fetch = async (url) => {
    if (url === "/status") requests++;
    return { json: async () => url === "/status" ? { balance: balance === null ? null : { display: balance } } : [] } as Response;
  };
  window.EventSource = class {};
  window.requestAnimationFrame = () => 0;
  window.setInterval = (handler, delay) => {
    if (delay === 30000 && typeof handler === "function") poll = () => { handler(); };
    return 0;
  };
  window.eval(window.document.querySelector("script")!.textContent!);
  const sidebar = window.document.querySelector<HTMLButtonElement>("#btn-balance")!;
  const footer = window.document.querySelector<HTMLButtonElement>("#balance-info")!;
  return {
    window, sidebar, footer,
    amount: () => window.document.querySelector("#sm-balance")!.textContent,
    requests: () => requests,
    refresh: async (value: string | null) => { balance = value; poll(); await setImmediate(); },
  };
}

test("web balances share a persistent toggle and refresh while masked", async (t) => {
  const saved = new Map<string, string>();
  const first = page(t, saved);
  assert.equal(first.sidebar.disabled, true);
  await setImmediate();
  assert.equal(first.amount(), "¥82.97");
  assert.equal(first.footer.textContent, "💰 ¥82.97");
  first.sidebar.click();
  assert.equal(first.amount(), "•••");
  assert.equal(first.footer.textContent, "💰 •••");
  assert.equal(saved.get(storageKey), "true");
  for (const button of [first.sidebar, first.footer]) {
    assert.equal(button.tagName, "BUTTON");
    assert.equal(button.getAttribute("aria-label"), "Show balance");
    assert.equal(button.title, "Show balance");
    assert.ok(!button.outerHTML.includes("82.97"));
    button.focus();
    assert.equal(first.window.document.activeElement, button);
  }
  const requests = first.requests();
  await first.refresh("¥81.50");
  assert.equal(first.requests(), requests + 1);
  assert.equal(first.amount(), "•••");
  assert.equal(first.footer.textContent, "💰 •••");
  first.footer.click();
  assert.equal(first.amount(), "¥81.50");
  assert.equal(first.footer.textContent, "💰 ¥81.50");
  assert.equal(first.requests(), requests + 1);
  assert.equal(saved.get(storageKey), "false");
  assert.equal(first.sidebar.title, "Hide balance");
  assert.equal(first.sidebar.getAttribute("aria-label"), "Hide balance: ¥81.50");
  first.footer.click();
  const reload = page(t, saved);
  await setImmediate();
  assert.equal(reload.amount(), "•••");
  assert.equal(reload.footer.textContent, "💰 •••");
  await reload.refresh(null);
  assert.equal(reload.amount(), "—");
  assert.equal(reload.footer.textContent, "");
  assert.equal(reload.sidebar.disabled, true);
  assert.equal(reload.footer.disabled, true);
});

test("blocked browser storage keeps the UI usable and reports persistence failures", async (t) => {
  const app = page(t, undefined, true, "zh");
  await setImmediate();
  assert.equal(app.amount(), "•••");
  assert.equal(app.sidebar.title, "显示余额");
  app.footer.click();
  assert.equal(app.amount(), "¥82.97");
  assert.equal(app.footer.title, "隐藏余额");
  app.sidebar.click();
  assert.equal(app.amount(), "•••");
  assert.equal(app.footer.textContent, "💰 •••");
  assert.match(app.window.document.querySelector(".notice--warn")!.textContent!, /无法保存余额显示偏好/);
});
