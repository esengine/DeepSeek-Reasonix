import assert from "node:assert/strict";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { presentError } from "../lib/errorPresentation";
import { ErrorMessage } from "../components/ErrorMessage";
import { LocaleProvider, preloadLocale, useI18n, type Locale, type Translator } from "../lib/i18n";
import { ToastProvider, useToast } from "../lib/toast";
import { en } from "../locales/en";
import { zh } from "../locales/zh";
import { zhTW } from "../locales/zh-TW";

const dictionaries = { en, zh, "zh-TW": zhTW };
for (const locale of ["en", "zh", "zh-TW"] as const) {
  const dict = dictionaries[locale];
  const translate: Translator = key => dict[key];
  for (const [error, key] of [
    ["dial tcp: connection refused", "error.connection"],
    [{ code: "transport_protocol", message: "connection error: PROTOCOL_ERROR" }, "error.transportProtocol"],
    ["lookup host: no such host", "error.dns"],
    ["deepseek: status 503: invalid api key mentioned in upstream diagnostics", "error.service"],
    ["HTTP 429: too many requests", "error.rateLimit"],
    ["HTTP 401", "error.auth"], ["HTTP 403", "error.forbidden"],
    ["HTTP 402", "error.quota"], ["HTTP 404", "error.endpoint"],
    ["request timed out", "error.timeout"], ["empty provider response", "error.empty"],
    ["no_deliverable_output: model finished without a visible final answer after recovery", "error.noDeliverable"],
    ["model stream interrupted: unexpected EOF", "error.interrupted"],
    ["open /tmp/file: permission denied", "error.permission"],
    ["open /tmp/file: no such file or directory", "error.fileMissing"],
    ["write /tmp/file: no space left on device", "error.diskFull"],
    ["HTTP 400: maximum context length exceeded", "error.context"],
    ["HTTP 429: insufficient_quota", "error.quota"],
    ["MCP server unavailable", "error.toolUnavailable"],
    ['plugin "fs": npm error code ECONNREFUSED request to https://registry.npmjs.org failed', "error.connection"],
    ["session changed on disk", "error.conflict"],
    [{ code: "ENOENT", message: "custom library message" }, "error.fileMissing"],
    [{ status: 503, message: "upstream busy" }, "error.service"],
  ] as const) {
    assert.equal(presentError(error, translate, locale).summary, dict[key], `${locale}: ${JSON.stringify(error)}`);
  }
  assert.deepEqual(presentError("opaque failure", translate, locale, { kind: "transport_protocol", transportCode: "PROTOCOL_ERROR" }), {
    summary: dict["error.transportProtocol"], detail: "opaque failure\nHTTP/2: PROTOCOL_ERROR",
  });
  assert.deepEqual(presentError("saved failure", translate, locale, { kind: "no_deliverable_output" }), {
    summary: dict["error.noDeliverable"], detail: "saved failure",
  });
  assert.deepEqual(presentError("Image processing failed: configured image understanding model does not accept images", translate, locale, { kind: "image_input" }), {
    summary: dict["error.imageInput"], detail: "Image processing failed: configured image understanding model does not accept images",
  });
  for (const [kind, key] of [
    ["request_send_timeout", "error.requestSendTimeout"],
    ["transport_eof", "error.transportEOF"],
    ["transport_reset", "error.transportReset"],
    ["transport_timeout", "error.timeout"],
    ["transport_dns", "error.dns"],
  ] as const) {
    assert.equal(presentError("saved provider failure", translate, locale, { kind }).summary, dict[key]);
  }
  assert.deepEqual(presentError("unknown provider failure", translate, locale, { kind: "future_transport_reason" }), presentError("unknown provider failure", translate, locale));
  assert.notEqual(presentError("connection error: PROTOCOL_ERROR", translate, locale).summary, dict["error.transportProtocol"], "untyped wording is not transport evidence");
  if (locale !== "en") {
    for (const raw of ["unknown backend failure", "read /tmp/timeout/404.txt: unsupported format", "read /tmp/中文文件: unsupported format", "<img src=x onerror=alert(1)>"]) {
      assert.deepEqual(presentError(raw, translate, locale), { summary: dict["error.unknown"], detail: raw });
    }
  }
}
assert.equal(presentError("文件操作失败：strange library exception", key => zh[key], "zh").summary, "文件操作失败");
assert.equal(presentError("Gateway: 模型/API 格式不匹配，请检查模型设置。", key => zh[key], "zh").summary, "模型/API 格式不匹配，请检查模型设置。");
for (const raw of ["文件操作失败：strange library exception", "Gateway: 模型/API 格式不匹配，请检查模型设置。", "操作失敗，請檢查設定。"]) {
  assert.deepEqual(presentError(raw, key => en[key], "en"), { summary: en["error.unknown"], detail: raw }, "English UI must not expose untranslated Chinese as its summary");
}
const original = Object.assign(new Error("upstream unavailable"), { code: "ECONNREFUSED" });
presentError(original, key => zh[key], "zh");
assert.equal(original.code, "ECONNREFUSED");
assert.equal(original.message, "upstream unavailable", "presentation must not rewrite control-flow errors");

const dom = new JSDOM('<!doctype html><html><body><div id="root"></div></body></html>', { url: "http://localhost/", pretendToBeVisual: true });
Object.assign(globalThis, { window: dom.window, document: dom.window.document, IS_REACT_ACT_ENVIRONMENT: true });
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
await Promise.all([preloadLocale("zh"), preloadLocale("zh-TW")]);
let changeLocale: (locale: Locale) => void;
let toast: ReturnType<typeof useToast>["showToast"];
const raw = "unknown library failure <img src=x onerror=alert(1)>";
const protocolError = "connection error: PROTOCOL_ERROR <img src=x onerror=alert(1)>";
const imageError = "Image processing failed: unknown vision model fixture/vision";
function Probe() {
  changeLocale = useI18n().setPref;
  toast = useToast().showToast;
  return <><p id="inline-error"><ErrorMessage error={raw} /></p><p id="transport-error"><ErrorMessage error={protocolError} diagnostic={{ kind: "transport_protocol", transportCode: "PROTOCOL_ERROR" }} /></p><p id="image-error"><ErrorMessage error={imageError} diagnostic={{ kind: "image_input" }} /></p><p id="summary-only"><ErrorMessage error={undefined} summary="仅有说明" /></p></>;
}
const root = createRoot(document.getElementById("root")!);
await act(async () => root.render(<LocaleProvider><ToastProvider><Probe /></ToastProvider></LocaleProvider>));
await act(async () => changeLocale("zh"));
assert.equal(document.querySelector("#inline-error .user-error__summary")?.textContent, zh["error.unknown"]);
assert.equal(document.querySelector(".user-error__detail"), null, "raw exception starts collapsed");
assert.equal(document.querySelector("#summary-only button"), null, "a summary without an error must not invent diagnostic details");
const toggle = document.querySelector<HTMLButtonElement>("#inline-error button")!;
await act(async () => toggle.click());
assert.equal(toggle.getAttribute("aria-expanded"), "true");
assert.equal(document.querySelector(".user-error__detail")?.textContent, raw);
assert.equal(document.querySelector("#inline-error img"), null, "diagnostics are text, never executable markup");
await act(async () => changeLocale("zh-TW"));
assert.equal(document.querySelector("#transport-error .user-error__summary")?.textContent, zhTW["error.transportProtocol"]);
assert.equal(document.querySelector("#inline-error .user-error__summary")?.textContent, zhTW["error.unknown"]);
assert.equal(document.querySelector(".user-error__detail")?.textContent, raw, "language switching preserves diagnostics");
await act(async () => changeLocale("en"));
assert.equal(document.querySelector("#transport-error .user-error__summary")?.textContent, en["error.transportProtocol"]);
await act(async () => document.querySelector<HTMLButtonElement>("#transport-error .user-error__toggle")!.click());
assert.equal(document.querySelector("#transport-error .user-error__detail")?.textContent, `${protocolError}\nHTTP/2: PROTOCOL_ERROR`);
assert.equal(document.querySelector("#transport-error img"), null);
assert.equal(document.querySelector("#image-error .user-error__summary")?.textContent, en["error.imageInput"]);
await act(async () => document.querySelector<HTMLButtonElement>("#image-error .user-error__toggle")!.click());
assert.equal(document.querySelector("#image-error .user-error__detail")?.textContent, imageError);
assert.equal(document.querySelector("#inline-error .user-error__summary")?.textContent, raw, "English errors follow the active English UI locale");
await act(async () => changeLocale("zh-TW"));

await act(async () => toast("HTTP 503: trace request-unique", "error", { durationMs: 80 }));
assert.equal(document.querySelector(".toast .user-error__summary")?.textContent, zhTW["error.service"]);
await act(async () => document.querySelector<HTMLButtonElement>(".toast .user-error__toggle")!.click());
await act(async () => new Promise(resolve => setTimeout(resolve, 100)));
assert.ok(document.querySelector(".toast"), "inspecting details pins the toast instead of dismissing it");
assert.match(document.querySelector(".toast .user-error__detail")!.textContent!, /request-unique/);
assert.equal(document.querySelector("button button"), null, "no nested buttons");
await act(async () => document.querySelector<HTMLButtonElement>('.toast button[aria-label="關閉提示"]')!.click());
assert.equal(document.querySelector(".toast"), null);
await act(async () => root.unmount());
dom.window.close();
console.log("PASS error presentation: three locales, known/unknown causes, unchanged errors, escaped details, live language switching and toast lifecycle");
